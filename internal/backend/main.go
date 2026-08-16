package backend

import (
	"fmt"
	"io"
	"io/fs"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/obegron/testtender/internal/model/types"
	secretpolicy "github.com/obegron/testtender/internal/policy/secret"
	"github.com/obegron/testtender/internal/util/podtemplate"
)

// Backend is the interface to orchestrate and manage kubernetes objects.
type Backend interface {
	StartContainer(*types.Container) (DeployState, error)
	AttachContainer(*types.Container, io.Reader, io.Writer, io.Writer, bool) error
	GetContainerStatus(*types.Container) (DeployState, error)
	CreatePortForwards(*types.Container)
	CreateReverseProxies(*types.Container)
	GetPodIP(*types.Container) (string, error)
	DeleteAll() error
	DeleteWithInstanceID(string) error
	DeleteContainer(*types.Container) error
	DeleteOlderThan(time.Duration) error
	WatchDeleteContainer(*types.Container) (chan struct{}, error)
	CopyFromContainer(*types.Container, string, io.Writer) error
	CopyToContainer(*types.Container, io.Reader, string, bool) error
	GetFileModeInContainer(tainr *types.Container, path string) (fs.FileMode, error)
	FileExistsInContainer(tainr *types.Container, path string) (bool, error)
	ExecContainer(*types.Container, *types.Exec, io.Reader, io.Writer) (int, error)
	GetLogs(*types.Container, *LogOptions, chan struct{}, io.Writer) error
	GetLogsRaw(*types.Container, *LogOptions, chan struct{}, io.Writer) error
	GetImageExposedPorts(string) (map[string]struct{}, error)
}

// instance is the internal representation of the Backend object.
type instance struct {
	cli               kubernetes.Interface
	cfg               *rest.Config
	podTemplate       *corev1.Pod
	containerTemplate corev1.Container
	initImage         string
	imagePullSecrets  []string
	secretPolicy      *secretpolicy.Policy
	namespace         string
	timeOut           int
	disableServices   bool
}

// Config is the structure to instantiate a Backend object
type Config struct {
	// Client is the kubernetes clientset
	Client kubernetes.Interface
	// RestConfig is the kubernetes config
	RestConfig *rest.Config
	// Namespace is the namespace in which all actions are performed
	Namespace string
	// ImagePullSecrets is an optional list of image pull secrets that need
	// to be added to the used pod templates
	ImagePullSecrets []string
	// AllowedSecrets is the exact list of namespace-local Secret names that
	// workloads may reference through testtender.io/secret-env.* labels.
	AllowedSecrets []string
	// InitImage is the image that is used as init container to prepare vols
	InitImage string
	// TimeOut is the max amount of time to wait until a container started
	// or deleted.
	TimeOut time.Duration
	// PodTemplate refers to an optional file containing a pod resource that
	// should be used as the base for creating pod resources.
	PodTemplate string
	// Disable the creation of services. A networking solution such as testtender-dns
	// should be used.
	DisableServices bool
}

// New will return a Backend instance.
func New(cfg Config) (Backend, error) {
	pod := &corev1.Pod{}
	if cfg.PodTemplate != "" {
		var err error
		pod, err = podtemplate.PodFromFile(cfg.PodTemplate)
		if err != nil {
			return nil, fmt.Errorf("error opening podtemplate: %w", err)
		}
	}
	secretPolicy, err := secretpolicy.New(cfg.AllowedSecrets)
	if err != nil {
		return nil, fmt.Errorf("configure Secret policy: %w", err)
	}

	return &instance{
		cli:               cfg.Client,
		cfg:               cfg.RestConfig,
		initImage:         cfg.InitImage,
		namespace:         cfg.Namespace,
		imagePullSecrets:  cfg.ImagePullSecrets,
		secretPolicy:      secretPolicy,
		podTemplate:       pod,
		containerTemplate: podtemplate.ContainerFromPod(pod),
		timeOut:           int(cfg.TimeOut.Seconds()),
		disableServices:   cfg.DisableServices,
	}, nil
}
