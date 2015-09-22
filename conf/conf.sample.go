package conf

import (
	"fmt"
	"os"
)

type Config struct {
	DbHost, Xpath, Tpath, UploadDir, MySQLHost, MySQLDB, MySQLUser, MySQLPass, Cypher, DemoUser string
}

var Conf Config

//init configuration
func Configuration() Config {
	name, err := os.Hostname()
	if err == nil {
		Conf.Cypher = "randomstring"
		Conf.DemoUser = "me@mydomain.co.uk"
		if name != "WIN-COMP-1234" {
			//PRODUCTION
			Conf.Tpath = "/path to templates directory/go/src/github.com/jezard/jps-go/tmpl/"
			Conf.Xpath = "/path to binaries directory/go/bin/"
			Conf.DbHost = "db1.joulepersecond.com"
			Conf.UploadDir = "php upload directory/uploads/"
			Conf.MySQLHost = "usual stuff"
			Conf.MySQLDB = "usual stuff"
			Conf.MySQLUser = "usual stuff"
			Conf.MySQLPass = "usual stuff"
		} else {
			Conf.Tpath = "path to templates directory/Go/src/github.com/jezard/jps-go/tmpl/"
			Conf.Xpath = "path to binaries directory/Go/bin/"
			Conf.DbHost = "127.0.0.1"
			Conf.UploadDir = "php upload directory/uploads/"
			Conf.MySQLHost = "usual stuff"
			Conf.MySQLDB = "usual stuff"
			Conf.MySQLUser = "usual stuff"
			Conf.MySQLPass = "usual stuff"
		}
	} else {
		fmt.Printf("Error: %v\n", err)
	}
	return Conf
}
