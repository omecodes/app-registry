package cmd

import (
	"database/sql"
	"encoding/json"
	"github.com/omecodes/app-registry/dao"
	"github.com/omecodes/bome"
	"github.com/omecodes/libome"
	"github.com/spf13/cobra"
	"io/ioutil"
	"log"
)

var input string

var appIDList []string

var appCMD = &cobra.Command{
	Use:   "apps",
	Short: "Manage applications store",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var addAppCMD = &cobra.Command{
	Use:   "add",
	Short: "Add or update applications info from json file",
	Run: func(cmd *cobra.Command, args []string) {
		err := application.InitDirs()
		if err != nil {
			log.Fatalln(err)
		}

		var list []*ome.Application

		inputBytes, err := ioutil.ReadFile(input)
		if err != nil {
			log.Fatalln(err)
		}

		err = json.Unmarshal(inputBytes, &list)
		if err != nil {
			log.Fatalln(err)
		}

		db, err := sql.Open("mysql", dsn)
		if err != nil {
			log.Fatalln(err)
		}

		appDB, err := dao.NewSQLApplicationsDB(db, bome.MySQL, "applications")
		if err != nil {
			log.Fatalln(err)
		}

		for _, a := range list {
			err = appDB.SaveApplication(a)
			if err != nil {
				log.Printf("could not save %s app: %s\n", a.Id, err)
			}
		}
	},
}

var delAppCMD = &cobra.Command{
	Use:   "del",
	Short: "Delete applications by ID",
	Run: func(cmd *cobra.Command, args []string) {
		err := application.InitDirs()
		if err != nil {
			log.Fatalln("could not initialize application dirs:", err)
		}

		db, err := sql.Open("mysql", dsn)
		if err != nil {
			log.Fatalln(err)
		}

		appDB, err := dao.NewSQLApplicationsDB(db, bome.MySQL, "applications")
		if err != nil {
			log.Fatalln(err)
		}

		for _, id := range appIDList {
			err = appDB.DeleteApplication(id)
			if err != nil {
				log.Printf("could not delete application %s: %s\n", id, err)
			}
		}
	},
}

func init() {
	appCMD.AddCommand(addAppCMD, delAppCMD)
	flags := appCMD.PersistentFlags()
	flags.StringVar(&dsn, "dsn", "", "DSN for the MySQL database (required)")
	_ = cobra.MarkFlagRequired(flags, "dsn")

	flags = addAppCMD.PersistentFlags()
	flags.StringVar(&input, "input", "", "Path to json file that contains application definitions")
	_ = cobra.MarkFlagRequired(flags, "input")

	flags = delAppCMD.PersistentFlags()
	flags.StringArrayVar(&appIDList, "ids", nil, "State of application id to delete")
	_ = cobra.MarkFlagRequired(flags, "ids")
}
