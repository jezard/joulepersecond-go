package dashboard

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gocql/gocql"
	"github.com/jezard/jps-go/conf"
	"github.com/jezard/jps-go/types" //?? http://grokbase.com/t/gg/golang-nuts/135g1sqdbr/go-nuts-using-a-struct-defined-in-a-package ??
	"github.com/jezard/jps-go/usersettings"
	"github.com/jezard/jps-go/utility"
	"html/template"
	"net/http"
	"strings"
	"time"
)

//struct for html page template
type Page struct {
	DashboardTvd types.Tvd
	ZoneData     types.Zones
	ZoneLabels   types.ZoneLabels
	Current_ff   types.Current_ff
	Settings     types.UserSettings
	Message      string
}

var config = conf.Configuration()

func DashboardHandler(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token")

	urlpath := r.URL.Path[1:] //get the path
	if urlpath == "favicon.ico" {
		http.NotFound(w, r)
	} else {
		urlparts := strings.Split(urlpath, "/")

		enc_uid := urlparts[1] //encoded user_id

		//get the user's settings from the MySQL Database
		user, err := Usersettings.Get(enc_uid) //gets the user in '@' format rather than '%40'

		//more settings
		user.Theme = "gray"
		if len(urlparts) > 2 {
			user.Theme = urlparts[2]
		} else {
			url_err := errors.New("Malformed URL")
			http.Error(w, url_err.Error(), http.StatusInternalServerError)
			return //malformed URL
		}

		data, zones, zonelabels := dashboard(user)
		current_ff := current_ff(user)

		if err != nil {
			fmt.Printf("%v\n", err)
			return
		}
		p := view(data, zones, zonelabels, current_ff, user)
		t, _ := template.ParseFiles(config.Tpath + "dashboard.html")
		t.Execute(w, p)

	}

}
func view(data types.Tvd, zones types.Zones, zoneLabels types.ZoneLabels, current_ff types.Current_ff, user types.UserSettings) (p Page) {
	//get any messages...
	db, err := sql.Open("mysql", config.MySQLUser+":"+config.MySQLPass+"@tcp("+config.MySQLHost+":3306)/"+config.MySQLDB)

	slug := "maintenance"
	var message string

	err = db.QueryRow("SELECT message FROM messages WHERE slug=?", slug).Scan(
		&message,
	)
	if err != nil {
		//job's a goodun
	}
	defer db.Close()

	p = Page{
		DashboardTvd: data,       //Tvd
		ZoneData:     zones,      //Zones
		ZoneLabels:   zoneLabels, //Zone labels!!!
		Current_ff:   current_ff,
		Settings:     user,
		Message:      message,
	}
	return
}

//get the week's activities
func dashboard(user types.UserSettings) (types.Tvd, types.Zones, types.ZoneLabels) {
	user_id := user.Id
	tvd_data_points := make([]types.Tvd_data_point, 0)
	tvd_data := make([]types.Tvd, 0)

	var user_data types.Metrics
	var activity_id string
	var end_summary_json []byte
	var activity_start time.Time
	var heart_json []byte
	var power_json []byte
	var cur_ftp int
	var cur_thr int
	var power_series []int
	var heart_series []int
	var has_power, has_heart bool

	var zoneData types.Zones

	cluster := gocql.NewCluster(config.DbHost)
	cluster.Keyspace = "joulepersecond"
	cluster.Consistency = gocql.Quorum
	session, _ := cluster.CreateSession()
	defer session.Close()

	//get all of the user's data (at least all for now) TODO limit these queries by date if poss. Done!
	timeNow := time.Now()

	//we can use TimeOffset to test from other dates
	timeNow = timeNow.AddDate(0, 0, user.TimeOffset)

	//timeTruncated is a time at the beginning of the day
	timeTruncated := timeNow.Truncate(time.Hour * 24)

	//we will use timeThen to refer to the beginning of the current week
	var timeThen time.Time
	dayOfWeek := int(timeTruncated.Weekday())
	if int(timeTruncated.Weekday()) != 0 { //if not equal to Sunday...
		timeThen = timeTruncated.AddDate(0, 0, -(dayOfWeek - 1)) //fetch records for the week so far (second -1 to start from Monday)
	} else {
		timeThen = timeTruncated.AddDate(0, 0, -6) //if today is Sunday, query back to Monday
	}

	iter := session.Query(`SELECT activity_id, activity_start, end_summary_json FROM joulepersecond.user_activity WHERE user_id = ? AND activity_start <=? AND activity_start >= ? `, user_id, timeNow, timeThen).Iter()
	for iter.Scan(&activity_id, &activity_start, &end_summary_json) {
		var tvd_data_point types.Tvd_data_point
		json.Unmarshal(end_summary_json, &user_data)

		tvd_data_point.Date = user_data.StartTime
		tvd_data_point.Dur = user_data.Dur
		if user_data.Utss > 0 {
			tvd_data_point.Tss = user_data.Utss
		} else if user_data.Tss > 0 {
			tvd_data_point.Tss = user_data.Tss
		} else if user_data.Etss > 0 {
			tvd_data_point.Tss = user_data.Etss
		} else {
			tvd_data_point.Tss = 0
		}
		tvd_data_points = append(tvd_data_points, tvd_data_point)

		//for each activity, get the exended data
		iter := session.Query(`SELECT power_json, heart_json, end_summary_json, has_power, has_heart, cur_ftp, cur_thr FROM joulepersecond.proc_activity WHERE activity_id = ? `, activity_id).Iter()
		for iter.Scan(&power_json, &heart_json, &end_summary_json, &has_power, &has_heart, &cur_ftp, &cur_thr) {
			json.Unmarshal(end_summary_json, &user_data)
			json.Unmarshal(power_json, &power_series)
			json.Unmarshal(heart_json, &heart_series)

			var samples int

			if has_power {
				samples = len(power_series)
				has_power = true
			}
			if has_heart {
				samples = len(heart_series)
				has_heart = true
			}
			if !has_heart && !has_power {
				break
			}

			if has_power {
				zoneData.HasPower = true
				var sum int
				var average float64
				for i := user.SampleSize; i < samples; i++ {
					//reset total
					sum = 0
					//get thirty second rolling slice
					rollingPowerSlice := power_series[i-user.SampleSize : i]
					for _, val := range rollingPowerSlice {
						//sum the sliding slice values
						sum += val
					}
					average = float64(sum / user.SampleSize)

					if average < 0.55*float64(cur_ftp) {
						zoneData.Z1++
					} else if average > 0.55*float64(cur_ftp) && average <= 0.74*float64(cur_ftp) {
						zoneData.Z2++
					} else if average > 0.74*float64(cur_ftp) && average <= 0.89*float64(cur_ftp) {
						zoneData.Z3++
					} else if average > 0.89*float64(cur_ftp) && average <= 1.04*float64(cur_ftp) {
						zoneData.Z4++
					} else if average > 1.04*float64(cur_ftp) && average <= 1.2*float64(cur_ftp) {
						zoneData.Z5++
					} else if average > 1.2*float64(cur_ftp) {
						zoneData.Z6++
					}
				}
			}

			//loop through each sample and post the value into the correct pidgeon hole
			if has_heart {
				zoneData.HasHeart = true
				for i := 0; i < samples; i++ {

					if float64(heart_series[i]) < 0.81*float64(cur_thr) {
						zoneData.HR1++
					} else if float64(heart_series[i]) > 0.81*float64(cur_thr) && float64(heart_series[i]) <= 0.89*float64(cur_thr) {
						zoneData.HR2++
					} else if float64(heart_series[i]) > 0.89*float64(cur_thr) && float64(heart_series[i]) <= 0.93*float64(cur_thr) {
						zoneData.HR3++
					} else if float64(heart_series[i]) > 0.93*float64(cur_thr) && float64(heart_series[i]) <= 0.99*float64(cur_thr) {
						zoneData.HR4++
					} else if float64(heart_series[i]) > 0.99*float64(cur_thr) && float64(heart_series[i]) <= 1.02*float64(cur_thr) {
						zoneData.HR5a++
					} else if float64(heart_series[i]) > 1.02*float64(cur_thr) && float64(heart_series[i]) <= 1.06*float64(cur_thr) {
						zoneData.HR5b++
					} else if float64(heart_series[i]) > 1.06*float64(cur_thr) {
						zoneData.HR5c++
					}
				}
			}
		}
	}

	//we now have all the data... Now sort it
	sumTss := 0
	var sumDur time.Duration
	var summedWeeklyTvd types.Tvd

	//loop through each retrieved activity
	for i := 0; i < len(tvd_data_points); i++ {
		tvd_data = append(tvd_data, summedWeeklyTvd)

		//sum the values
		sumTss += tvd_data_points[i].Tss
		sumDur += tvd_data_points[i].Dur
	}
	summedWeeklyTvd.TotalTss = sumTss
	summedWeeklyTvd.TotalDur = utility.Round(sumDur.Hours(), .5, 2)

	var zoneLabels types.ZoneLabels
	zoneLabels.PowerZ1 = int(0.55 * float64(user.Ftp))
	zoneLabels.PowerZ2 = int(0.74 * float64(user.Ftp))
	zoneLabels.PowerZ3 = int(0.89 * float64(user.Ftp))
	zoneLabels.PowerZ4 = int(1.04 * float64(user.Ftp))
	zoneLabels.PowerZ5 = int(1.2 * float64(user.Ftp))
	zoneLabels.HeartZ1 = int(0.81 * float64(user.Thr))
	zoneLabels.HeartZ2 = int(0.89 * float64(user.Thr))
	zoneLabels.HeartZ3 = int(0.93 * float64(user.Thr))
	zoneLabels.HeartZ4 = int(0.99 * float64(user.Thr))
	zoneLabels.HeartZ5a = int(1.02 * float64(user.Thr))
	zoneLabels.HeartZ5b = int(1.06 * float64(user.Thr))

	//get the power and heartrate zone data
	return summedWeeklyTvd, zoneData, zoneLabels
}
func current_ff(user types.UserSettings) types.Current_ff {
	user_id := user.Id
	ff_data := make([]types.Ff_data_point, 0)      //activity day
	ff_scan_data := make([]types.Ff_data_point, 0) //all days in range

	var current_ff types.Current_ff

	var user_data types.Metrics
	var end_summary_json []byte
	var has_power, has_heart bool
	var activity_start time.Time
	var firstDate time.Time
	var firstIter = true

	var activity_id string
	var day = time.Hour * 24

	timeNow := time.Now()
	var recentMonths = timeNow.AddDate(0, -6, 0)

	cluster := gocql.NewCluster(config.DbHost)
	cluster.Keyspace = "joulepersecond"
	cluster.Consistency = gocql.Quorum
	session, _ := cluster.CreateSession()
	defer session.Close()

	//get all of the user's data (at least all for now) TODO limit these queries by date if poss.
	iter := session.Query(`SELECT activity_start, activity_id, end_summary_json, has_power, has_heart FROM joulepersecond.user_activity WHERE user_id = ? AND activity_start > ? ORDER BY activity_start ASC`, user_id, recentMonths).Iter()
	//loop through each activity
	for iter.Scan(&activity_start, &activity_id, &end_summary_json, &has_power, &has_heart) {
		if firstIter {
			firstDate = activity_start
			firstIter = false
		}
		var ff_data_point types.Ff_data_point
		//unmarshal the activtiy metrics
		json.Unmarshal(end_summary_json, &user_data)

		//values for each scanned activity
		var user_tss int

		//check for a tss override value
		session.Query(`SELECT activity_id, tss_value FROM activity_meta WHERE activity_id = ?`, activity_id).Scan(&activity_id, &user_tss)

		//get a value for tss whether from hr, power or (TODO user set)
		if user_tss > 0 {
			ff_data_point.Tss = user_tss
		} else {
			if has_power {
				ff_data_point.Tss = user_data.Tss
			} else if has_heart {
				ff_data_point.Tss = user_data.Etss
			}
		}
		//get the date
		ff_data_point.Date = user_data.StartTime

		//add date and tss to array slice
		ff_data = append(ff_data, ff_data_point)

	}
	if err := iter.Close(); err != nil {
		fmt.Printf("%v\n", err)
	}
	//calulate the number of days between first recorded activity and current date
	duration := time.Since(firstDate) //get total duration in time.Duration
	days := int(duration/day) + 1     //add one day to include current day

	//loop through each day.
	for i := 0; i <= days+30; i++ {
		//create a var to hold a sample for each day scanned - it may or maynot contain an activity, but will still contain ctl, atl and tsb vals along with the date
		var scan_data_point types.Ff_data_point

		//get each day to be scanned (all days between first activity and current date)
		scanDate := firstDate.AddDate(0, 0, i)
		scan_data_point.Date = scanDate                 //get the date
		scanYear, scanMonth, scanDay := scanDate.Date() //get date elements that are easier to compare

		//now get each of the days where we have an activity
		for _, val := range ff_data {

			actYear, actMonth, actDay := val.Date.Date()
			if actYear == scanYear && actMonth == scanMonth && actDay == scanDay {
				//these are the days where we have activites - add the TSS

				//we might have more than one activity in a day so append any others TSS on this day
				scan_data_point.Tss += val.Tss

			}
			//else leave TSS as it is (0)
		}
		if i > 0 { //don't have a yesterday value on the first day
			scan_data_point.Atl = ff_scan_data[i-1].Atl + (float64(scan_data_point.Tss)-ff_scan_data[i-1].Atl)/float64(user.Atl_constant)
			scan_data_point.Ctl = ff_scan_data[i-1].Ctl + (float64(scan_data_point.Tss)-ff_scan_data[i-1].Ctl)/float64(user.Ctl_constant)
			scan_data_point.Tsb = scan_data_point.Ctl - scan_data_point.Atl
		}

		//save off today's fitness and freshness...
		timeNow := time.Now()
		if scanDate.Year() == timeNow.Year() && scanDate.Month() == timeNow.Month() && scanDate.Day() == timeNow.Day() {
			current_ff.Atl = int(scan_data_point.Atl)
			current_ff.Ctl = int(scan_data_point.Ctl)
			current_ff.Tsb = int(scan_data_point.Tsb)

		}
		//add the data to our array slice
		ff_scan_data = append(ff_scan_data, scan_data_point)

	}
	return current_ff

}
