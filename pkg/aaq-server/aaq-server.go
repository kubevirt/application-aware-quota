package aaq_server

import (
	"fmt"
	"github.com/rs/cors"
	"io"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/certificate"
	"k8s.io/klog/v2"
	"kubevirt.io/applications-aware-quota/pkg/util"
	"net/http"
)

const (
	healthzPath = "/healthz"
	ServePath   = "/serve-path"
)

// Server is the public interface to the upload proxy
type Server interface {
	Start() error
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type AAQServer struct {
	bindAddress       string
	bindPort          uint
	secretCertManager certificate.Manager
	handler           http.Handler
	aaqNS             string
}

// AaqServer returns an initialized uploadProxyApp
func AaqServer(aaqNS string,
	bindAddress string,
	bindPort uint,
	secretCertManager certificate.Manager,
	aaqCli kubernetes.Interface,
) (Server, error) {
	app := &AAQServer{
		secretCertManager: secretCertManager,
		bindAddress:       bindAddress,
		bindPort:          bindPort,
		aaqNS:             aaqNS,
	}
	app.initHandler(aaqCli)

	return app, nil
}

func (app *AAQServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	app.handler.ServeHTTP(w, r)
}

func (app *AAQServer) initHandler(aaqCli kubernetes.Interface) {
	mux := http.NewServeMux()
	mux.HandleFunc(healthzPath, app.handleHealthzRequest)
	mux.Handle(ServePath, NewAaqServerHandler(app.aaqNS, aaqCli))
	app.handler = cors.AllowAll().Handler(mux)

}

func (app *AAQServer) handleHealthzRequest(w http.ResponseWriter, r *http.Request) {
	_, err := io.WriteString(w, "OK")
	if err != nil {
		klog.Errorf("handleHealthzRequest: failed to send response; %v", err)
	}
}

func (app *AAQServer) Start() error {
	return app.startTLS()
}

func (app *AAQServer) startTLS() error {
	var serveFunc func() error
	bindAddr := fmt.Sprintf("%s:%d", app.bindAddress, app.bindPort)
	tlsConfig := util.SetupTLS(app.secretCertManager)
	server := &http.Server{
		Addr:      bindAddr,
		Handler:   app.handler,
		TLSConfig: tlsConfig,
	}

	serveFunc = func() error {
		return server.ListenAndServeTLS("", "")
	}

	errChan := make(chan error)

	go func() {
		errChan <- serveFunc()
	}()
	// wait for server to exit
	return <-errChan
}
