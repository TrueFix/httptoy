package server

import (
	"net/http"
	"os"

	"github.com/TrueFix/httptoy/pkg/metrics"
	"github.com/TrueFix/httptoy/pkg/routes"
	"github.com/gorilla/mux"
)

func instrumentRouter(r *mux.Router) *metrics.Registry {
	templates := []string{"/metrics"}
	if err := r.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		template, err := route.GetPathTemplate()
		if err != nil {
			return err
		}
		templates = append(templates, template)
		return nil
	}); err != nil {
		panic(err)
	}
	meter := metrics.New(os.Getenv("APP_VERSION"), os.Getenv("GIT_SHA"), templates, func() metrics.Resources {
		snapshot := routes.ResourceSnapshot()
		resources := metrics.Resources{SessionsActive: snapshot.SessionsActive, CaptureBytes: snapshot.CaptureBytes, CapturedRequests: snapshot.CapturedRequests, IdempotencyKeys: snapshot.IdempotencyKeys, SSEClients: snapshot.SSEClients}
		for _, pool := range snapshot.Pools {
			resources.Pools = append(resources.Pools, metrics.Pool{Name: pool.Name, Active: pool.Active, Capacity: pool.Capacity, Rejected: pool.Rejected})
		}
		return resources
	})
	r.Handle("/metrics", meter).Methods(http.MethodGet)
	r.Use(observeRoute)
	return meter
}
