package cmd

import (
	"context"
	"fmt"
	"github.com/omecodes/service"
	"path/filepath"

	"github.com/omecodes/app-registry/server"
	"github.com/omecodes/common/env/app"
	"github.com/omecodes/common/utils/log"
	"github.com/omecodes/common/utils/prompt"
	"github.com/omecodes/libome/ports"
	"github.com/spf13/cobra"
)

const (
	Vendor  = "Ome"
	Version = "1.0.0"
)

var (
	domain       string
	ip, eip      string
	hPort        int
	gPort        int
	acm          bool
	dsn          string
	regAddr      string
	certFilename string
	keyFilename  string
	cmd          *cobra.Command
)

var application *app.App

func init() {
	application = app.New(Vendor, "AppRegistry",
		app.WithVersion(Version),
		app.WithRunCommandFunc(start),
	)
	cmd = application.GetCommand()
	cmd.AddCommand(appCMD)

	flags := application.StartCommand().PersistentFlags()
	flags.StringVar(&domain, "dn", "", "Domain name (required)")
	flags.StringVar(&ip, "ip", "", "IP address to bind server to (required)")
	flags.StringVar(&eip, "eip", "", "External IP address")
	flags.BoolVar(&acm, "acme", false, "Use acme auto-cert loading")
	flags.IntVar(&hPort, "http", ports.OmeHTTP, "HTTP server port")
	flags.IntVar(&gPort, "grpc", ports.Ome, "gRPC server port")
	flags.StringVar(&dsn, "dsn", "", "DSN for the MySQL database (required)")
	flags.StringVar(&regAddr, "registry", "", "Address to start registry server on")
	flags.StringVar(&certFilename, "cert", "", "Certificate file path")
	flags.StringVar(&keyFilename, "key", "", "Key file path")

	_ = cobra.MarkFlagRequired(flags, "domain")
	_ = cobra.MarkFlagRequired(flags, "ip")
	_ = cobra.MarkFlagRequired(flags, "dsn")
}

func start() {
	if dsn == "" || ip == "" || domain == "" {
		sc := application.StartCommand()
		_ = sc.Help()
		return
	}

	var boxParams service.Params
	log.File = filepath.Join(application.DataDir(), "run.log")
	boxParams.Dir = application.DataDir()
	boxParams.Name = "Ome"
	boxParams.Acme = acm
	boxParams.RegistrySecure = true
	boxParams.Domain = domain
	boxParams.Ip = ip
	if eip != "" && eip != ip {
		boxParams.ExternalIp = eip
	}
	if regAddr == "" {
		regAddr = fmt.Sprintf("%s:%d", boxParams.Domain, ports.Discover)
	}
	boxParams.RegistryAddress = regAddr

	box, err := service.CreateBox(context.Background(), &boxParams)
	if err != nil {
		log.Fatal("could not create box", log.Err(err))
	}

	s := server.New(&server.Config{
		TLSCertFilename: certFilename,
		TLSKeyFilename:  keyFilename,
		Application:     application,
		DSN:             dsn,
		Box:             box,
		WebPort:         hPort,
		GRPCPort:        gPort,
	})
	err = s.Start()
	if err != nil {
		log.Fatal("could not start server", log.Err(err))
	}

	defer s.Stop()
	<-prompt.QuitSignal()
}

func Execute() error {
	return cmd.Execute()
}
