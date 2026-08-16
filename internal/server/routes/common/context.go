package common

import (
	"errors"
	"net/http"

	"golang.org/x/time/rate"
	"k8s.io/klog"

	"github.com/obegron/testtender/internal/backend"
	"github.com/obegron/testtender/internal/events"
	"github.com/obegron/testtender/internal/model"
	"github.com/obegron/testtender/internal/model/types"
	imagepolicy "github.com/obegron/testtender/internal/policy/image"
	secretpolicy "github.com/obegron/testtender/internal/policy/secret"
)

const (
	// DefaultPollRate defines maximum polling request per second towards the backend
	DefaultPollRate = 1
	// DefaultPollBurst defines maximum burst poll requests towards the backend
	DefaultPollBurst = 3
)

// Config is the structure to instantiate a Router object
type Config struct {
	// ImageResolver authorizes requested images and resolves mirror rewrites.
	ImageResolver imagepolicy.Resolver
	// SecretPolicy authorizes namespace-local Secret references without reading
	// Secret objects or values.
	SecretPolicy *secretpolicy.Policy
	// Inspector specifies if the image inspect feature is enabled
	Inspector bool
	// PortForward specifies if the the services should be port-forwarded
	PortForward bool
	// ReverseProxy enables a reverse-proxy to the services via 0.0.0.0 on the testtender host
	ReverseProxy bool
	// RequestCPU contains an optional default k8s cpu request
	RequestCPU string
	// RequestMemory contains an optional default k8s memory request
	RequestMemory string
	// RequestEphemeralStorage contains an optional default k8s ephemeral-storage request
	RequestEphemeralStorage string
	// RunasUser contains the UID to run pods as
	RunasUser string
	// PullPolicy contains the default pull policy for images
	PullPolicy string
	// PreArchive will enable copying files without starting containers
	PreArchive bool
	// ServiceAccount contains the service account name to be used for running containers
	ServiceAccount string
	// ActiveDeadlineSeconds contains the active deadline seconds to be used for running containers
	ActiveDeadlineSeconds int64
	// NamePrefix contains a prefix for the names used for the container deployments (optional).
	NamePrefix string
	// NodeSelector contains a comma-separated list of key=value pairs that is used to schedule pods to specific nodes
	NodeSelector string
	// IgnoreContainerMemory is used to ignore Docker memory settings and use requests/limits from TestTender config
	IgnoreContainerMemory bool
	// PollRate defines maximum polling requests per second towards the backend.
	// Defaults to DefaultPollRate if zero.
	PollRate float64
	// PollBurst defines the maximum burst of poll requests towards the backend.
	// Defaults to DefaultPollBurst if zero.
	PollBurst int
}

// ContextRouter is the object that contains shared context for the testtender API endpoints.
type ContextRouter struct {
	Config  Config
	DB      *model.Database
	Backend backend.Backend
	Events  events.Events
	Limiter *rate.Limiter
	// SecretPolicy validates Secret-backed environment references at container
	// creation; the backend validates them again while constructing the Pod.
	SecretPolicy *secretpolicy.Policy
}

// NewContextRouter will instantiate a ContextRouter object.
func NewContextRouter(kub backend.Backend, cfg Config) (*ContextRouter, error) {
	if cfg.ImageResolver == nil {
		cfg.ImageResolver = imagepolicy.Passthrough()
	}
	if cfg.SecretPolicy == nil {
		var err error
		cfg.SecretPolicy, err = secretpolicy.New(nil)
		if err != nil {
			return nil, err
		}
	}
	db, err := model.New()
	if err != nil {
		return nil, err
	}
	pollRate := cfg.PollRate
	if pollRate <= 0 {
		pollRate = DefaultPollRate
	}
	pollBurst := cfg.PollBurst
	if pollBurst <= 0 {
		pollBurst = DefaultPollBurst
	}
	cr := &ContextRouter{
		Config:       cfg,
		DB:           db,
		Backend:      kub,
		Events:       events.New(),
		Limiter:      rate.NewLimiter(rate.Limit(pollRate), pollBurst),
		SecretPolicy: cfg.SecretPolicy,
	}
	return cr, nil
}

// ResolveImage authorizes and resolves an image requested through either the
// Docker or libpod compatibility API.
func (in *ContextRouter) ResolveImage(requested string) (string, error) {
	resolved, err := in.Config.ImageResolver.Resolve(requested)
	if err != nil {
		klog.Warningf("image policy: requested=%q outcome=denied error=%q", requested, err)
		return "", err
	}
	klog.Infof("image policy: requested=%q resolved=%q outcome=allowed", requested, resolved)
	return resolved, nil
}

// ImageErrorStatus maps policy denials and malformed image names to stable API
// responses. A denial is authorization failure; malformed client input is a
// bad request.
func ImageErrorStatus(err error) int {
	var denied *imagepolicy.DeniedError
	if errors.As(err, &denied) {
		return http.StatusForbidden
	}
	return http.StatusBadRequest
}

// ValidateContainerRequest enforces Kubernetes-only product boundaries before
// any compatibility-route state is persisted.
func ValidateContainerRequest(container *types.Container) error {
	if container.HasDockerSockBinding() {
		return errors.New("mounting /var/run/docker.sock is unsupported: TestTender runs containers only as Kubernetes Pods")
	}
	return nil
}

// ValidateContainerRequest enforces request boundaries that depend on the
// configured policies before compatibility state is persisted.
func (in *ContextRouter) ValidateContainerRequest(container *types.Container) error {
	if err := ValidateContainerRequest(container); err != nil {
		return err
	}
	return in.SecretPolicy.Validate(container.Labels, container.GetEnvVar())
}

// ContainerRequestErrorStatus maps request-policy errors to stable API status
// codes. An allowlist rejection is authorization failure; malformed references
// are bad client input.
func ContainerRequestErrorStatus(err error) int {
	var denied *secretpolicy.DeniedError
	if errors.As(err, &denied) {
		return http.StatusForbidden
	}
	return http.StatusBadRequest
}
