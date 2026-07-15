// Package tlsreload reloads the serving keypair when cert-manager or the
// OpenShift service-ca operator rotates the mounted Secret.
package tlsreload

import (
	"crypto/tls"
	"log/slog"
	"sync"
	"time"
)

type Reloader struct {
	mu       sync.RWMutex
	cert     *tls.Certificate
	certFile string
	keyFile  string
}

func New(certFile, keyFile string, log *slog.Logger) (*Reloader, error) {
	r := &Reloader{certFile: certFile, keyFile: keyFile}
	if err := r.reload(); err != nil {
		return nil, err
	}
	go func() {
		for range time.Tick(time.Minute) {
			if err := r.reload(); err != nil {
				log.Error("reloading TLS cert failed", "error", err)
			}
		}
	}()
	return r, nil
}

func (r *Reloader) reload() error {
	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.cert = &cert
	r.mu.Unlock()
	return nil
}

func (r *Reloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cert, nil
}
