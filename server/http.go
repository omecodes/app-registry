package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/omecodes/common/errors"
	"github.com/omecodes/common/httpx"
	"github.com/omecodes/common/utils/log"
	"github.com/omecodes/libome"
)

const (
	APIRoute  = "/api/"
	InfoRoute = "/info"
)

func (s *Server) createRouter(m *runtime.ServeMux) http.Handler {
	r := mux.NewRouter()
	r.PathPrefix(APIRoute).Handler(m)
	r.HandleFunc(InfoRoute, s.serveInfo)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		r.ServeHTTP(w, req)
		duration := time.Since(start)
		log.Info(
			req.Method+" "+req.RequestURI,
			log.Field("params", req.URL.RawQuery),
			log.Field("handler", gatewayServiceName),
			log.Field("duration", duration.String()),
		)
	})
}

func (s *Server) serveInfo(w http.ResponseWriter, r *http.Request) {
	info := &ome.Info{}
	registry := s.config.Box.Registry()

	address, err := s.config.Box.ServiceAddress("ca")
	if err != nil {
		log.Error("could not get Certificate Signing Service address", log.Err(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	info.CSR = fmt.Sprintf("grpc://%s", address)

	dataInfo, err := registry.FirstOfType(ome.ServiceType_Data)
	if err != nil && !errors.IsNotFound(err) {
		log.Error("could not get data service", log.Err(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if dataInfo != nil {
		for _, node := range dataInfo.Nodes {
			if node.Protocol == ome.Protocol_Http {
				info.Data.HTTP = node.Address
			} else if node.Protocol == ome.Protocol_Grpc {
				info.Data.GRPC = node.Address
			}
		}
	}

	accountsInfo, err := registry.FirstOfType(ome.ServiceType_Authentication)
	if err != nil && !errors.IsNotFound(err) {
		log.Error("could not get account service", log.Err(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if accountsInfo != nil {
		node := accountsInfo.Nodes[0]
		info.Registration = fmt.Sprintf("https://%s/api/account/new", node.Address)
		info.Oauth2.Endpoints = ome.Endpoints{
			Authorize: fmt.Sprintf("https://%s/authorize", node.Address),
			Token:     fmt.Sprintf("https://%s/token", node.Address),
			Revoke:    fmt.Sprintf("https://%s/token/revoke", node.Address),
		}
		info.Oauth2.SignatureKey = node.Meta[ome.MetaTokenVerifyingKey]
	}

	jwtStoreInfo, err := registry.FirstOfType(ome.ServiceType_TokenStore)
	if err != nil && !errors.IsNotFound(err) {
		log.Error("could not get token store service", log.Err(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if jwtStoreInfo != nil {
		for _, node := range jwtStoreInfo.Nodes {
			fmt.Println(node.Id)
			if node.Protocol == ome.Protocol_Http {
				if node.Security == ome.Security_Insecure {
					info.Oauth2.Endpoints.Verify = fmt.Sprintf("http://%s/jwt/match", node.Address)
				} else {
					info.Oauth2.Endpoints.Verify = fmt.Sprintf("https://%s/jwt/match", node.Address)
				}
			}
		}
	}

	httpx.WriteJSON(w, http.StatusOK, info)
}
