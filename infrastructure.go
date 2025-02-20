package cke

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cybozu-go/etcdutil"
	"github.com/cybozu-go/log"
	"github.com/cybozu-go/well"
	vault "github.com/hashicorp/vault/api"
	clientv3 "go.etcd.io/etcd/client/v3"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var httpClient = &well.HTTPClient{
	Client: &http.Client{},
}

var vaultClient atomic.Value

type certCache struct {
	cert      []byte
	key       []byte
	timestamp time.Time
	lifetime  time.Duration
}

func (c *certCache) get(issue func() (cert, key []byte, err error)) (cert, key []byte, err error) {
	now := time.Now()
	if c.cert != nil {
		if now.Sub(c.timestamp) < c.lifetime {
			return c.cert, c.key, nil
		}
	}

	cert, key, err = issue()
	if err == nil {
		c.cert = cert
		c.key = key
		c.timestamp = now
	}
	return
}

// KubeHTTP provides TLS client certificate to access kube-apiserver.
// The certificate is cached in memory in order to avoid excessive certificate issuance.
type KubeHTTP struct {
	once   sync.Once
	cache  *certCache
	err    error
	ca     string
	client *well.HTTPClient
}

// Init initializes KubeHTTP.
func (k *KubeHTTP) Init(ctx context.Context, inf Infrastructure) error {
	k.once.Do(func() {
		cache := &certCache{
			lifetime: time.Hour * 24,
		}
		k.cache = cache

		ca, err := inf.Storage().GetCACertificate(ctx, CAKubernetes)
		if err != nil {
			k.err = err
			return
		}
		k.ca = ca

		cp := x509.NewCertPool()
		cp.AppendCertsFromPEM([]byte(k.ca))
		k.client = &well.HTTPClient{
			Client: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						RootCAs: cp,
					},
				},
			},
		}
	})

	return k.err
}

// CACert returns the CA certificate of kube-apiserver.
func (k *KubeHTTP) CACert() string {
	return k.ca
}

// GetCert retrieves cached TLS client certificate to access kube-apiserver.
func (k *KubeHTTP) GetCert(ctx context.Context, inf Infrastructure) (cert, key []byte, err error) {
	issue := func() (cert, key []byte, err error) {
		c, k, e := KubernetesCA{}.IssueUserCert(ctx, inf, RoleAdmin, AdminGroup, "25h")
		if e != nil {
			return nil, nil, e
		}
		return []byte(c), []byte(k), nil
	}
	return k.cache.get(issue)
}

// Client returns a HTTP client to acess kube-apiserver.
func (k *KubeHTTP) Client() *well.HTTPClient {
	return k.client
}

func setVaultClient(client *vault.Client) {
	vaultClient.Store(client)
}

func getVaultClient() (*vault.Client, error) {
	v := vaultClient.Load()
	if v == nil {
		return nil, errors.New("vault is not connected")
	}
	return v.(*vault.Client), nil
}

// Infrastructure presents an interface for infrastructure on CKE
type Infrastructure interface {
	Close()

	// Agent returns the agent corresponding to addr and returns nil if addr is not connected.
	Agent(addr string) Agent
	Engine(addr string) ContainerEngine
	Vault() (*vault.Client, error)
	Storage() Storage

	NewEtcdClient(ctx context.Context, endpoints []string) (*clientv3.Client, error)
	K8sConfig(ctx context.Context, n *Node) (*rest.Config, error)
	K8sClient(ctx context.Context, n *Node) (*kubernetes.Clientset, error)
	HTTPClient() *well.HTTPClient
	HTTPSClient(ctx context.Context) (*well.HTTPClient, error)
}

var kubeHTTP KubeHTTP

type ckeInfrastructure struct {
	agents  map[string]Agent
	storage Storage

	etcdOnce sync.Once
	etcdErr  error
	serverCA string
	etcdCert string
	etcdKey  string

	// following fields are accessed by multiple goroutines, hence
	// they need to be guarded by sync.Once.
	once     sync.Once
	initErr  error
	kubeCert []byte
	kubeKey  []byte
}

func (i *ckeInfrastructure) init(ctx context.Context) error {
	if err := kubeHTTP.Init(ctx, i); err != nil {
		return err
	}

	i.once.Do(func() {
		cert, key, err := kubeHTTP.GetCert(ctx, i)
		if err != nil {
			i.initErr = err
			return
		}

		i.kubeCert = cert
		i.kubeKey = key
	})
	return i.initErr
}

// NewInfrastructure creates a new Infrastructure instance
func NewInfrastructure(ctx context.Context, c *Cluster, s Storage) (Infrastructure, error) {
	vc, err := getVaultClient()
	if err != nil {
		return nil, err
	}

	secret, err := vc.Logical().Read(SSHSecret)
	if err != nil {
		return nil, err
	}
	privkeys := secret.Data

	agents := make(map[string]Agent)
	defer func() {
		for _, a := range agents {
			a.Close()
		}
	}()

	mu := new(sync.Mutex)

	env := well.NewEnvironment(ctx)
	for _, n := range c.Nodes {
		node := n
		env.Go(func(ctx context.Context) error {
			mykey, ok := privkeys[node.Address]
			if !ok {
				mykey = privkeys[""]
			}
			if mykey == nil {
				return errors.New("no ssh private key for " + node.Address)
			}
			a, err := SSHAgent(node, mykey.(string))
			if err != nil {
				log.Warn("failed to create SSHAgent for "+node.Address, map[string]interface{}{
					log.FnError: err,
				})
				// lint:ignore nilerr  Just skip adding my agent to agents.
				return nil
			}

			mu.Lock()
			agents[node.Address] = a
			mu.Unlock()
			return nil
		})

	}
	env.Stop()
	err = env.Wait()
	if err != nil {
		return nil, err
	}

	// This assignment of the `agent` must be placed last.
	inf := &ckeInfrastructure{agents: agents, storage: s}
	agents = nil
	return inf, nil
}

func (i *ckeInfrastructure) Agent(addr string) Agent {
	return i.agents[addr]
}

func (i *ckeInfrastructure) Engine(addr string) ContainerEngine {
	return Docker(i.agents[addr])
}

func (i *ckeInfrastructure) Vault() (*vault.Client, error) {
	return getVaultClient()
}

func (i *ckeInfrastructure) Storage() Storage {
	return i.storage
}

func (i *ckeInfrastructure) Close() {
	for _, a := range i.agents {
		a.Close()
	}
	i.agents = nil
}

func (i *ckeInfrastructure) NewEtcdClient(ctx context.Context, endpoints []string) (*clientv3.Client, error) {
	i.etcdOnce.Do(func() {
		serverCA, err := i.Storage().GetCACertificate(ctx, CAServer)
		if err != nil {
			i.etcdErr = err
			return
		}
		etcdCert, etcdKey, err := EtcdCA{}.IssueRoot(ctx, i)
		if err != nil {
			i.etcdErr = err
			return
		}

		i.serverCA = serverCA
		i.etcdCert = etcdCert
		i.etcdKey = etcdKey
	})

	if i.etcdErr != nil {
		return nil, i.etcdErr
	}

	cfg := &etcdutil.Config{
		Endpoints: endpoints,
		Timeout:   etcdutil.DefaultTimeout,
		TLSCA:     i.serverCA,
		TLSCert:   i.etcdCert,
		TLSKey:    i.etcdKey,
	}
	return etcdutil.NewClient(cfg)
}

func (i *ckeInfrastructure) K8sConfig(ctx context.Context, n *Node) (*rest.Config, error) {
	if err := i.init(ctx); err != nil {
		return nil, err
	}

	return &rest.Config{
		Host: "https://" + n.Address + ":6443",
		TLSClientConfig: rest.TLSClientConfig{
			CertData: i.kubeCert,
			KeyData:  i.kubeKey,
			CAData:   []byte(kubeHTTP.CACert()),
		},
		Timeout: 5 * time.Second,
	}, nil
}

func (i *ckeInfrastructure) K8sClient(ctx context.Context, n *Node) (*kubernetes.Clientset, error) {
	cfg, err := i.K8sConfig(ctx, n)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

func (i *ckeInfrastructure) HTTPClient() *well.HTTPClient {
	return httpClient
}

func (i *ckeInfrastructure) HTTPSClient(ctx context.Context) (*well.HTTPClient, error) {
	err := i.init(ctx)
	if err != nil {
		return nil, err
	}
	return kubeHTTP.Client(), nil
}
