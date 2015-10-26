/* Deals with the user and their settings, will eventually do away with the horrible cookie based system used*/
package Usersettings

import (
	"database/sql"
	//"fmt"
	_ "github.com/go-sql-driver/mysql" //go get github.com/go-sql-driver/mysql
	"github.com/jezard/joulepersecond-go/conf"
	"github.com/jezard/joulepersecond-go/types"
	"github.com/jezard/joulepersecond-go/utility"
	"net/url"
	"strings"
	"time"
)

func Get(enc_uid string) (user types.UserSettings, _err error) {
	conf := conf.Configuration()

	//get the decoded user email (id)
	uid, err := utility.Decode(enc_uid)

	if uid == "" {
		uid = conf.DemoUser
		user.Demo = true
	}

	db, err := sql.Open("mysql", conf.MySQLUser+":"+conf.MySQLPass+"@tcp("+conf.MySQLHost+":3306)/"+conf.MySQLDB)

	var paid_account bool
	var my_ftp, my_thr, my_rhr, my_weight, set_ncp_rolloff, my_age, set_data_cutoff int
	var set_autofill, my_gender string
	var my_vo2 float32

	err = db.QueryRow("SELECT paid_account, my_ftp, my_thr, my_rhr, my_weight, set_ncp_rolloff, set_autofill, set_data_cutoff, my_age, my_vo2, my_gender FROM user WHERE email=?", uid).Scan(
		&paid_account,
		&my_ftp,
		&my_thr,
		&my_rhr,
		&my_weight,
		&set_ncp_rolloff,
		&set_autofill,
		&set_data_cutoff,
		&my_age,
		&my_vo2,
		&my_gender,
	)
	if err != nil {
		_err = err
	}
	defer db.Close()

	user.EncId = url.QueryEscape(enc_uid)
	user.Email = uid
	user.Id = strings.Replace(uid, "@", "%40", 1) //respecting the initial cookie based auth..
	user.Paid_account = paid_account
	user.Ftp = my_ftp
	user.Thr = my_thr
	user.Rhr = my_rhr
	user.Weight = my_weight
	user.Ncp_rolloff = set_ncp_rolloff
	user.Autofill = set_autofill
	user.Stopgap = time.Duration(set_data_cutoff)
	user.Age = my_age
	user.Vo2 = my_vo2
	user.Gender = my_gender

	//hardcoded (for now) settings
	user.Atl_constant = 7
	user.Ctl_constant = 42
	user.SampleSize = 5
	user.TimeOffset = 0 //eg 0, -1, -2 etc... or 7 go forward a week

	return

}
