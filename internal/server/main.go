package server

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"k8s.io/klog"

	"github.com/obegron/testtender/internal/auth"
	"github.com/obegron/testtender/internal/backend"
	imagepolicy "github.com/obegron/testtender/internal/policy/image"
	secretpolicy "github.com/obegron/testtender/internal/policy/secret"
	"github.com/obegron/testtender/internal/server/httputil"
	"github.com/obegron/testtender/internal/server/routes"
	"github.com/obegron/testtender/internal/server/routes/common"
)

// Server is the API server.
type Server struct {
	kub backend.Backend
}

// New will instantiate a Server object.
func New(kub backend.Backend) *Server {
	return &Server{kub: kub}
}

// Run will initialize the http api server and configure all available
// routers.
func (s *Server) Run(ctx context.Context) error {
	if !klog.V(2) {
		gin.SetMode(gin.ReleaseMode)
	}

	resolver, err := imagepolicy.LoadFile(viper.GetString("registry.policy-file"))
	if err != nil {
		return fmt.Errorf("load image policy: %w", err)
	}
	if resolver.Enabled() {
		klog.Infof("image policy enabled from %s", viper.GetString("registry.policy-file"))
	} else {
		klog.Warning("image policy disabled; requested image names are not restricted")
	}
	allowedSecretsRaw := strings.ReplaceAll(viper.GetString("kubernetes.allowed-secrets"), " ", "")
	allowedSecrets := []string{}
	if allowedSecretsRaw != "" {
		allowedSecrets = strings.Split(allowedSecretsRaw, ",")
	}
	secretPolicy, err := secretpolicy.New(allowedSecrets)
	if err != nil {
		return fmt.Errorf("load Secret policy: %w", err)
	}
	if len(allowedSecrets) > 0 {
		klog.Infof("Secret environment references enabled for %d exact Secret names", len(allowedSecrets))
	}
	allowedSubjectsRaw := strings.ReplaceAll(viper.GetString("oidc.allowed-subjects"), " ", "")
	allowedSubjects := []string{}
	if allowedSubjectsRaw != "" {
		allowedSubjects = strings.Split(allowedSubjectsRaw, ",")
	}
	allowedNamespacesRaw := strings.ReplaceAll(viper.GetString("oidc.allowed-namespaces"), " ", "")
	allowedNamespaces := []string{}
	if allowedNamespacesRaw != "" {
		allowedNamespaces = strings.Split(allowedNamespacesRaw, ",")
	}
	verifier, err := auth.New(ctx, auth.Config{
		Required:          viper.GetBool("oidc.required"),
		Issuer:            viper.GetString("oidc.issuer"),
		DiscoveryURL:      viper.GetString("oidc.discovery-url"),
		Audience:          viper.GetString("oidc.audience"),
		AllowedSubjects:   allowedSubjects,
		AllowedNamespaces: allowedNamespaces,
		CAFile:            viper.GetString("oidc.ca-file"),
		HTTPTimeout:       viper.GetDuration("oidc.http-timeout"),
		RefreshInterval:   viper.GetDuration("oidc.jwks-refresh-interval"),
		ClockSkew:         viper.GetDuration("oidc.clock-skew"),
	})
	if err != nil {
		return fmt.Errorf("initialize OIDC authentication: %w", err)
	}
	if verifier == nil {
		klog.Warning("OIDC authentication disabled; any client with network access can call the API")
	} else {
		klog.Infof("OIDC authentication enabled for issuer %s, audience %s, %d exact subjects and %d exact namespaces",
			viper.GetString("oidc.issuer"), viper.GetString("oidc.audience"), len(allowedSubjects), len(allowedNamespaces))
	}

	router := s.getGinEngine(resolver, secretPolicy, verifier)
	router.SetTrustedProxies(nil)

	socket := viper.GetString("server.socket")
	port := viper.GetString("server.listen-addr")

	tls := viper.GetBool("server.tls-enable")
	cert := viper.GetString("server.tls-cert-file")
	key := viper.GetString("server.tls-key-file")

	errch := make(chan error, 1)

	go func() {
		if tls {
			errch <- router.RunTLS(port, cert, key)
		} else {
			errch <- router.Run(port)
		}
		klog.Infof("api server started listening on %s", port)
	}()

	if socket != "" {
		go func() {
			errch <- router.RunUnix(socket)
		}()
		klog.Infof("api server started listening on %s", socket)
	}

	err = nil
	select {
	case err = <-errch:
		break
	case <-ctx.Done():
		break
	}

	if socket != "" {
		if err := os.Remove(socket); err != nil {
			klog.Errorf("error removing socket: %s", err)
		}
	}

	return err
}

// getGinEngine will return a gin.Engine router and configure the
// appropriate middleware.
func (s *Server) getGinEngine(imageResolver imagepolicy.Resolver, secretPolicy *secretpolicy.Policy, verifier *auth.Verifier) *gin.Engine {
	router := gin.New()
	router.Use(httputil.VersionAliasMiddleware(router))
	router.Use(gin.Logger())
	router.Use(httputil.RequestLoggerMiddleware())
	router.Use(httputil.ResponseLoggerMiddleware())
	router.Use(gin.Recovery())
	if verifier != nil {
		router.Use(auth.Middleware(verifier))
	}

	insp := viper.GetBool("registry.inspector")
	if insp {
		klog.Infof("image inspector enabled")
	}

	pfwrd := viper.GetBool("port-forward")
	if pfwrd {
		klog.Infof("port-forwarding services to 127.0.0.1")
	}

	revprox := viper.GetBool("reverse-proxy")
	if revprox && !pfwrd {
		klog.Infof("enabled reverse-proxy services via 0.0.0.0 on the testtender host")
	}
	if revprox && pfwrd {
		klog.Infof("ignored reverse-proxy as port-forward is enabled")
		revprox = false
	}

	prea := viper.GetBool("pre-archive")
	if prea {
		klog.Infof("copying archives without starting containers enabled")
	}

	reqcpu := viper.GetString("kubernetes.request-cpu")
	if reqcpu != "" {
		klog.Infof("default cpu request: %s", reqcpu)
	}
	reqmem := viper.GetString("kubernetes.request-memory")
	if reqmem != "" {
		klog.Infof("default memory request: %s", reqmem)
	}
	reqes := viper.GetString("kubernetes.request-ephemeral-storage")
	if reqes != "" {
		klog.Infof("default ephemeral-storage request: %s", reqes)
	}

	runasuid := viper.GetString("kubernetes.runas-user")
	if runasuid != "" {
		klog.Infof("default runas user: %s", runasuid)
	}

	nodesel := viper.GetString("kubernetes.node-selector")
	if nodesel != "" {
		klog.Infof("default node selector: %s", nodesel)
	}

	pulpol := viper.GetString("kubernetes.pull-policy")
	klog.Infof("default image pull policy: %s", pulpol)

	sa := viper.GetString("kubernetes.service-account")
	klog.Infof("service account used in deployments: %s", sa)

	podprfx := viper.GetString("kubernetes.pod-name-prefix")
	klog.Infof("pod name prefix: %s", podprfx)

	ads := viper.GetInt64("kubernetes.active-deadline-seconds")

	icm := viper.GetBool("ignore-container-memory")

	pollRate := viper.GetFloat64("server.poll-rate")
	pollBurst := viper.GetInt("server.poll-burst")

	klog.Infof("using namespace: %s", viper.GetString("kubernetes.namespace"))

	cr, err := common.NewContextRouter(s.kub, common.Config{
		ImageResolver:           imageResolver,
		SecretPolicy:            secretPolicy,
		Inspector:               insp,
		RequestCPU:              reqcpu,
		RequestMemory:           reqmem,
		RequestEphemeralStorage: reqes,
		ServiceAccount:          sa,
		RunasUser:               runasuid,
		NodeSelector:            nodesel,
		PullPolicy:              pulpol,
		PortForward:             pfwrd,
		ReverseProxy:            revprox,
		PreArchive:              prea,
		NamePrefix:              podprfx,
		ActiveDeadlineSeconds:   ads,
		IgnoreContainerMemory:   icm,
		PollRate:                pollRate,
		PollBurst:               pollBurst,
	})
	if err != nil {
		klog.Errorf("error setting up context: %s", err)
	}

	routes.RegisterDockerRoutes(router, cr)
	routes.RegisterLibpodRoutes(router, cr)

	return router
}
