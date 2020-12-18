package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/gorilla/sessions"
	"github.com/omecodes/app-registry/dao"
	"github.com/omecodes/bome"
	"github.com/omecodes/common/env/app"
	"github.com/omecodes/common/errors"
	"github.com/omecodes/common/utils/log"
	"github.com/omecodes/libome"
	"github.com/omecodes/service"
	"google.golang.org/grpc"
)

type Config struct {
	TLSCertFilename string
	TLSKeyFilename  string
	DSN             string
	Box             *service.Box
	Application     *app.App
	WebPort         int
	GRPCPort        int
}

type Server struct {
	config        *Config
	gRPCHandler   ome.ApplicationsServer
	appsDB        dao.ApplicationsDB
	translationDB *bome.DoubleMap

	certsCacheDir string
	cookieStore   *sessions.CookieStore
	initialized   bool
}

func New(cfg *Config) *Server {
	return &Server{
		config: cfg,
	}
}

func (s *Server) init() error {
	if s.initialized {
		return nil
	}

	s.initialized = true
	var err error

	a := s.config.Application

	s.certsCacheDir = filepath.Join(a.DataDir(), "certs")
	err = os.MkdirAll(s.certsCacheDir, os.ModePerm)
	if err != nil {
		return err
	}

	db, err := sql.Open(bome.MySQL, s.config.DSN)
	if err != nil {
		return err
	}

	s.appsDB, err = dao.NewSQLApplicationsDB(db, bome.MySQL, "applications")
	if err != nil {
		return err
	}

	s.translationDB, err = bome.NewDoubleMap(db, bome.MySQL, "attr_translations")
	if err != nil {
		return err
	}

	cookiesKeyFilename := filepath.Join(s.config.Application.DataDir(), "cookies.key")
	cookiesKey, err := ioutil.ReadFile(cookiesKeyFilename)
	if err != nil {
		cookiesKey = make([]byte, 64)
		_, err = rand.Read(cookiesKey)
		if err != nil {
			log.Error("could not generate secret key for web cookies", log.Err(err))
			return err
		}
		err = ioutil.WriteFile(cookiesKeyFilename, cookiesKey, os.ModePerm)
		if err != nil {
			log.Info("could not save secret key for web cookies", log.Field("error", err))
		}
	}
	s.cookieStore = sessions.NewCookieStore(cookiesKey)

	application, err := s.appsDB.GetApplication("ome")
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	if application == nil {
		secretBytes := make([]byte, 8)
		_, err := rand.Read(secretBytes)
		if err != nil {
			return err
		}
		application = new(ome.Application)
		application.Id = "ome"
		application.Activated = true
		application.OauthCallbackUrl = "https://accounts.ome.ci"
		application.Level = ome.ApplicationLevel_Master
		application.Secret = base64.RawStdEncoding.EncodeToString(secretBytes)
		application.Info = &ome.AppInfo{
			ApplicationId: "ome",
			CreatedBy:     "ome",
			CreatedAt:     0,
			Label:         "Accounts Application",
			LogoUrl:       "https://ca.ome.ci/public/logo/accounts.png",
			Description:   "Accounts management application",
			Website:       "https://ca.ome.ci",
		}
		err = s.appsDB.SaveApplication(application)
		if err != nil {
			return err
		}

		secretFilename := filepath.Join(s.config.Application.DataDir(), "ome-app.secret")
		_ = ioutil.WriteFile(secretFilename, []byte(application.Secret), os.ModePerm)
	}
	s.gRPCHandler = NewApplicationServerGRPCHandler(s.appsDB, s.cookieStore, s.translationDB)
	return nil
}

func (s *Server) Start() error {
	err := s.init()
	if err != nil {
		return err
	}

	err = s.config.Box.StartCAService(func(cred *ome.ProxyCredentials) (bool, error) {
		if cred == nil {
			return false, errors.Unauthorized
		}

		a, err := s.appsDB.GetApplication(cred.Key)
		if err != nil {
			log.Error("could not get secret", log.Err(err), log.Field("for", cred.Key))
			if errors.IsNotFound(err) {
				return false, errors.Forbidden
			}
			return false, errors.Internal
		}
		return a.Secret == cred.Secret, nil
	})
	if err != nil {
		return err
	}

	registry := s.config.Box.Registry()
	var registryID string
	registryID = registry.RegisterEventHandler(ome.EventHandlerFunc(func(event *ome.RegistryEvent) {
		if event.ServiceId == s.config.Box.Name() && (event.Type == ome.RegistryEventType_Register || event.Type == ome.RegistryEventType_Update) {
			registry.DeregisterEventHandler(registryID)

			if s.config.Box.AcmeEnabled() {
				err = s.config.Box.StartAcmeServiceGatewayMapping(&service.ACMEServiceGatewayParams{
					ForceRegister:  true,
					ServiceName:    s.config.Box.Name(),
					TargetNodeName: gRPCServiceName,
					NodeName:       secureGatewayServiceName,
					Binder:         ome.RegisterApplicationsHandlerFromEndpoint,
					MuxWrapper:     s.createRouter,
				})
			} else {
				err = s.config.Box.StartGatewayGrpcMappingNode(&service.GatewayGrpcMappingParams{
					ForceRegister:  true,
					ServiceName:    s.config.Box.Name(),
					TargetNodeName: gRPCServiceName,
					NodeName:       secureGatewayServiceName,
					Port:           s.config.WebPort,
					Security:       ome.Security_Tls,
					Binder:         ome.RegisterApplicationsHandlerFromEndpoint,
					MuxWrapper:     s.createRouter,
				})
			}
			if err != nil {
				log.Error("could not start gateway", log.Err(err))
			}
		}
	}))

	err = s.config.Box.StartGrpcNode(&service.GrpcNodeParams{
		ForceRegister: true,
		RegisterHandlerFunc: func(gs *grpc.Server) {
			ome.RegisterApplicationsServer(gs, s.gRPCHandler)
		},
		ServiceType: ome.ServiceType_AppStore,
		Port:        s.config.GRPCPort,
		Node: &ome.Node{
			Id:       gRPCServiceName,
			Protocol: ome.Protocol_Grpc,
			Security: ome.Security_MutualTls,
			Ttl:      -1,
		},
	})
	if err != nil {
		log.Error("could not start gRPC server", log.Err(err), log.Field("service", gRPCServiceName))
		return errors.New("failed to start server")
	}
	return nil
}

func (s *Server) Stop() {
	s.config.Box.Stop()
}
