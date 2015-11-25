package analysis

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/gocql/gocql"
	"github.com/jezard/joulepersecond-go/conf"
	"github.com/jezard/joulepersecond-go/types"
	"github.com/jezard/joulepersecond-go/usersettings"
	"github.com/jezard/joulepersecond-go/utility"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

//data for Critical Power chart (multiple activity)
type CpRow struct {
	CpTime [3]int //in google chart timeofday format
	CpVal  int
	CpAhr  int
	CpAcad int
}

type Cp3 struct {
	CpTime   int //in seconds
	CpPower1 int //power series 1
	CpLabel1 string
	CpPower2 int //power series 2
	CpLabel2 string
	CpPower3 int //power series 3
	CpLabel3 string
}
type Cp3Legend struct {
	Series1, Series2, Series3 string
}

type Hvp struct {
	AvHeartRate      int
	AvPower          int
	AvCadence        int
	Day, Month, Year int
	NotableCp        float64
	CpHr             int //the average heart rate for the notable critical power value
	CpCad            int
	HasValue         bool
	Meta             ActivityMeta
}

type Tvd_data_point struct {
	Date time.Time
	Tss  int
	Dur  time.Duration
}

type Tvd struct {
	TimeLabel string
	TotalTss  int
	TotalDur  float64 //in hours
}

type Hpbz struct { //merged store of data for Heart/Power by zone chart
	StartTime                                                                                                                           time.Time
	Has_heart, Has_power                                                                                                                bool
	Samples                                                                                                                             int //in seconds to calculate average
	CountPZ1, CountPZ2, CountPZ3, CountPZ4, CountPZ5, CountPZ6, CountHZ1, CountHZ2, CountHZ3, CountHZ4, CountHZ5a, CountHZ5b, CountHZ5c int
}

type Pbz struct {
	TimeLabel                          string
	AvZ1, AvZ2, AvZ3, AvZ4, AvZ5, AvZ6 float64
}

type Hbz struct {
	TimeLabel                                   string
	AvZ1, AvZ2, AvZ3, AvZ4, AvZ5a, AvZ5b, AvZ5c float64
}

//store various types of information about an activity - 1 record per activity
type ActivityMeta struct {
	ActivityName, ActivityID                      string
	TssOverride, MotivationLevel, PerceivedEffort int
	IndoorRide, OutdoorRide, Race, Train          bool
}

//struct for html page template
type Page struct {
	Title                           string
	EndSummary                      types.Metrics
	FfData                          []types.Ff_data_point
	CpData                          []Cp3
	HvpData                         []Hvp
	TvdData                         []Tvd
	DashboardTvd                    Tvd
	CpLegend1, CpLegend2, CpLegend3 string
	TvdLegend                       string
	HvpLabel                        string
	PbzData                         []Pbz
	HbzData                         []Hbz
	Settings                        types.UserSettings
	Filter                          Filter
	ZoneLabels                      types.ZoneLabels
	StandardRidesHTML               template.HTML
}
type Filter struct { //need to refactor some of the filters in Page struct into here...
	Race, Train, Indoor, Outdoor, HeartData                              bool
	Historylen                                                           int  //filter value
	ShowTss, ShowMmp, ShowDur, ShowPbz, ShowHbz, ShowHvp, HasGraphOutput bool //whether or not to process and show graphs
	S5, S20, S60, S300, S1200, S3600                                     bool
	CpFilter                                                             int  //5,20,60 sec etc...
	ShowCPs                                                              bool //whether user wishes to show notable CPs on graph
	HvpTo, HvpFrom                                                       int  //time in minutes
	OffsetDays                                                           int  //number of days to end filter period
	StandardRides                                                        []int
}

//create a data type to represent aggregated sample data
type Samples struct {
	Power, Hr, Cad, Samplecount, Freewheelcount int
}

//general
var day = time.Hour * 24
var history_days = 90
var config = conf.Configuration()

func AnalysisHandler(w http.ResponseWriter, r *http.Request) {
	// http://stackoverflow.com/questions/12830095/setting-http-headers-in-golang
	if origin := r.Header.Get("Origin"); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token")

	var user types.UserSettings //user info
	var filter Filter           //filter settings
	filter.Historylen = history_days

	urlpath := r.URL.Path[1:] //get the path (after the domain:port slash)/eg view/activity/ActIviTyiD
	urlparts := strings.Split(urlpath, "/")
	encoded_user_id, _ := url.QueryUnescape(urlparts[1])
	user, _ = Usersettings.Get(encoded_user_id)

	if urlpath == "favicon.ico" {
		http.NotFound(w, r)
	} else {

		//get the tss value if set
		hisLen, err := strconv.Atoi(r.FormValue("history-len"))
		if err == nil {
			filter.Historylen = hisLen
		}
		//get the cp-filter value
		cpFilter, err := strconv.Atoi(r.FormValue("cp-filter"))
		if err == nil {
			filter.CpFilter = cpFilter
			filter.ShowCPs = true
			switch filter.CpFilter {
			case 5:
				filter.S5 = true
				break
			case 20:
				filter.S20 = true
				break
			case 60:
				filter.S60 = true
				break
			case 300:
				filter.S300 = true
				break
			case 1200:
				filter.S1200 = true
				break
			case 3600:
				filter.S3600 = true
				break
			default:
				filter.ShowCPs = false
			}
		}
		//get the show hide filters
		showTss := r.FormValue("show-tss")
		if showTss == "checked" {
			filter.ShowTss = true
		}
		showMmp := r.FormValue("show-mmp")
		if showMmp == "checked" {
			filter.ShowMmp = true
		}
		showDur := r.FormValue("show-dur")
		if showDur == "checked" {
			filter.ShowDur = true
		}
		showPbz := r.FormValue("show-pbz")
		if showPbz == "checked" {
			filter.ShowPbz = true
		}
		showHbz := r.FormValue("show-hbz")
		if showHbz == "checked" {
			filter.ShowHbz = true
		}
		showHvp := r.FormValue("show-hvp")
		if showHvp == "checked" {
			filter.ShowHvp = true
		}
		hvpFrom, err := strconv.Atoi(r.FormValue("hvp-from"))
		if err == nil {
			filter.HvpFrom = hvpFrom
		}
		hvpTo, err := strconv.Atoi(r.FormValue("hvp-to"))
		if err == nil {
			filter.HvpTo = hvpTo
		} else {
			hvpTo = 300
			filter.HvpTo = hvpTo
		}

		offsetDays, err := strconv.Atoi(r.FormValue("offset-days"))
		if err == nil {
			filter.OffsetDays = offsetDays
		} else {
			offsetDays = 0
			filter.OffsetDays = offsetDays
		}

		//more filters
		if r.Method == "POST" {
			indoor := r.FormValue("is-indoor")
			if indoor == "true" {
				filter.Indoor = true
			} else {
				filter.Indoor = false
			}

			outdoor := r.FormValue("is-outdoor")
			if outdoor == "true" {
				filter.Outdoor = true
			} else {
				filter.Outdoor = false
			}

			race := r.FormValue("is-race")
			if race == "true" {
				filter.Race = true
			} else {
				filter.Race = false
			}

			train := r.FormValue("is-training")
			if train == "true" {
				filter.Train = true
			} else {
				filter.Train = false
			}

			heart := r.FormValue("has-heart")
			if heart == "true" {
				filter.HeartData = true
			} else {
				filter.HeartData = false
			}

		} else {
			filter.Indoor = true
			filter.Outdoor = true
			filter.Race = true
			filter.Train = true
			filter.HeartData = true
		}

		if !filter.ShowTss && !filter.ShowMmp && !filter.ShowDur && !filter.ShowPbz && !filter.ShowHbz && !filter.ShowHvp {
			filter.HasGraphOutput = false
		} else {
			filter.HasGraphOutput = true
		}

		filter.ShowCPs = true
		if r.FormValue("cp-filter") == "" {
			filter.S3600 = true
		}
		filter.ShowHvp = true
		filter.HasGraphOutput = true

		theme, err := r.Cookie("theme")
		if err != nil {
			user.Theme = "gray"
		} else {
			user.Theme = theme.Value
		}

		//standard rides
		if err := r.ParseForm(); err != nil {
			// handle error
		}

		//only restrict filter to subscribers if over max value
		if filter.Historylen > history_days {

			//send the email address to the php app for validation
			resp, err := http.PostForm("http://joulepersecond.com/getstatus", url.Values{"email": {user.Id}})
			if err != nil {
				fmt.Printf("%v", err)
			}
			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)

			//hash a copy of what is returned by php and the should match if user is
			hasher := md5.New()
			hasher.Write([]byte("and the email is: " + user.Id))

			if string(body) != hex.EncodeToString(hasher.Sum(nil)) {
				//reset to maximum of 90 days for non subscribers
				if filter.Historylen > history_days {
					filter.Historylen = history_days
				}
			}
		}

		sr := r.Form["standard_rides[]"]
		for i := 0; i < len(sr); i++ {
			standard_ride, _ := strconv.Atoi(sr[i])
			filter.StandardRides = append(filter.StandardRides, standard_ride)
		}

		p := view(user, filter)
		t, _ := template.ParseFiles(config.Tpath + "analysis.html")
		t.Execute(w, p)

	}
}

//heart/Power by zone
func hpbz(user types.UserSettings, filter Filter) ([]Hbz, []Pbz) {
	user_id := user.Id

	var user_data types.Metrics
	var end_summary_json []byte
	var heart_json []byte
	var power_json []byte
	var cur_ftp int
	var cur_thr int
	var power_series []int
	var heart_series []int
	var has_power, has_heart bool
	var activity_id string
	var activity_start time.Time

	hbz_data := make([]Hbz, 0)
	pbz_data := make([]Pbz, 0)

	var temp_row Hpbz
	temp_rows := make([]Hpbz, 0)

	cluster := gocql.NewCluster(config.DbHost)
	cluster.Keyspace = "joulepersecond"
	cluster.Consistency = gocql.Quorum
	session, _ := cluster.CreateSession()
	defer session.Close()
	var sH1, sH2, sH3, sH4, sH5a, sH5b, sH5c, sP1, sP2, sP3, sP4, sP5, sP6 int

	timeNow := time.Now()
	timeThen := timeNow.AddDate(0, 0, -filter.Historylen)

	//get all of the user's data (at least all for now) TODO limit these queries by date if poss.
	iter := session.Query(`SELECT activity_start, activity_id FROM joulepersecond.user_activity WHERE user_id = ? AND activity_start > ? ORDER BY activity_start ASC`, user_id, timeThen).Iter()

	for iter.Scan(&activity_start, &activity_id) {

		iter := session.Query(`SELECT power_json, heart_json, end_summary_json, has_power, has_heart, cur_ftp, cur_thr FROM joulepersecond.proc_activity WHERE activity_id = ? `, activity_id).Iter()
		for iter.Scan(&power_json, &heart_json, &end_summary_json, &has_power, &has_heart, &cur_ftp, &cur_thr) {
			json.Unmarshal(end_summary_json, &user_data)
			json.Unmarshal(power_json, &power_series)
			json.Unmarshal(heart_json, &heart_series)

			temp_row.StartTime = activity_start
			//TODO next: Split all time series data in to zones and add it to temp_row/(s) for further date processing

			if has_power {
				temp_row.Samples = len(power_series)
				temp_row.Has_power = true
			}
			if has_heart {
				temp_row.Samples = len(heart_series)
				temp_row.Has_heart = true
			}
			if !has_heart && !has_power {
				break
			}

			//clear the values
			temp_row.CountPZ1 = 0
			temp_row.CountPZ2 = 0
			temp_row.CountPZ3 = 0
			temp_row.CountPZ4 = 0
			temp_row.CountPZ5 = 0
			temp_row.CountPZ6 = 0

			temp_row.CountHZ1 = 0
			temp_row.CountHZ2 = 0
			temp_row.CountHZ3 = 0
			temp_row.CountHZ4 = 0
			temp_row.CountHZ5a = 0
			temp_row.CountHZ5b = 0
			temp_row.CountHZ5c = 0

			if has_power {

				var sum int
				var average float64
				for i := user.SampleSize; i < temp_row.Samples; i++ {
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
						temp_row.CountPZ1++
					} else if average > 0.55*float64(cur_ftp) && average <= 0.74*float64(cur_ftp) {
						temp_row.CountPZ2++
					} else if average > 0.74*float64(cur_ftp) && average <= 0.89*float64(cur_ftp) {
						temp_row.CountPZ3++
					} else if average > 0.89*float64(cur_ftp) && average <= 1.04*float64(cur_ftp) {
						temp_row.CountPZ4++
					} else if average > 1.04*float64(cur_ftp) && average <= 1.2*float64(cur_ftp) {
						temp_row.CountPZ5++
					} else if average > 1.2*float64(cur_ftp) {
						temp_row.CountPZ6++
					}
				}
			}

			//loop through each sample and post the value into the correct pidgeon hole
			for i := 0; i < temp_row.Samples; i++ {

				if has_heart {
					if float64(heart_series[i]) < 0.81*float64(cur_thr) {
						temp_row.CountHZ1++
					} else if float64(heart_series[i]) > 0.81*float64(cur_thr) && float64(heart_series[i]) <= 0.89*float64(cur_thr) {
						temp_row.CountHZ2++
					} else if float64(heart_series[i]) > 0.89*float64(cur_thr) && float64(heart_series[i]) <= 0.93*float64(cur_thr) {
						temp_row.CountHZ3++
					} else if float64(heart_series[i]) > 0.93*float64(cur_thr) && float64(heart_series[i]) <= 0.99*float64(cur_thr) {
						temp_row.CountHZ4++
					} else if float64(heart_series[i]) > 0.99*float64(cur_thr) && float64(heart_series[i]) <= 1.02*float64(cur_thr) {
						temp_row.CountHZ5a++
					} else if float64(heart_series[i]) > 1.02*float64(cur_thr) && float64(heart_series[i]) <= 1.06*float64(cur_thr) {
						temp_row.CountHZ5b++
					} else if float64(heart_series[i]) > 1.06*float64(cur_thr) {
						temp_row.CountHZ5c++
					}
				}

			}
			temp_rows = append(temp_rows, temp_row)

		}

	}
	clearVals := func() {
		sH1 = 0
		sH2 = 0
		sH3 = 0
		sH4 = 0
		sH5a = 0
		sH5b = 0
		sH5c = 0
		sP1 = 0
		sP2 = 0
		sP3 = 0
		sP4 = 0
		sP5 = 0
		sP6 = 0
	}
	//so now for each activity we have the sum of each of the zones (value for each second * number seconds) and the number of seconds to divide by later once summed by date
	//loope through each retrieved activity
	var lastActivity time.Time
	var numResult int

	for i := 1; i < len(temp_rows); i++ {
		if i == 1 {
			lastActivity = temp_rows[i].StartTime
		}

		//if we want to show data in monthly format
		if filter.Historylen > 366 { //show over 365 days as monthly
			//set last activity on first iteration only
			thisDate := temp_rows[i].StartTime
			prevDate := lastActivity

			//if we're still in the current month sum these values
			if thisDate.Month() != prevDate.Month() || i == len(temp_rows)-1 {
				var summedMonthlyHbz Hbz
				var summedMonthlyPbz Pbz

				summedMonthlyHbz.AvZ1 = utility.Round((float64(sH1) / 3600.0), .5, 2)
				summedMonthlyHbz.AvZ2 = utility.Round((float64(sH2) / 3600.0), .5, 2)
				summedMonthlyHbz.AvZ3 = utility.Round((float64(sH3) / 3600.0), .5, 2)
				summedMonthlyHbz.AvZ4 = utility.Round((float64(sH4) / 3600.0), .5, 2)
				summedMonthlyHbz.AvZ5a = utility.Round((float64(sH5a) / 3600.0), .5, 2)
				summedMonthlyHbz.AvZ5b = utility.Round((float64(sH5b) / 3600.0), .5, 2)
				summedMonthlyHbz.AvZ5c = utility.Round((float64(sH5c) / 3600.0), .5, 2)

				summedMonthlyPbz.AvZ1 = utility.Round((float64(sP1) / 3600.0), .5, 2)
				summedMonthlyPbz.AvZ2 = utility.Round((float64(sP2) / 3600.0), .5, 2)
				summedMonthlyPbz.AvZ3 = utility.Round((float64(sP3) / 3600.0), .5, 2)
				summedMonthlyPbz.AvZ4 = utility.Round((float64(sP4) / 3600.0), .5, 2)
				summedMonthlyPbz.AvZ5 = utility.Round((float64(sP5) / 3600.0), .5, 2)
				summedMonthlyPbz.AvZ6 = utility.Round((float64(sP6) / 3600.0), .5, 2)

				var month time.Month
				var year string

				if thisDate.Month() != prevDate.Month() {
					month = prevDate.Month()
					year = strconv.Itoa(prevDate.Year())
				} else {
					month = thisDate.Month()
					year = strconv.Itoa(thisDate.Year())
				}

				monthStr := month.String()
				summedMonthlyHbz.TimeLabel = monthStr[0:3] + " '" + year[2:4]
				summedMonthlyPbz.TimeLabel = monthStr[0:3] + " '" + year[2:4]
				clearVals()

				hbz_data = append(hbz_data, summedMonthlyHbz)
				pbz_data = append(pbz_data, summedMonthlyPbz)

				//reset the last activity date for the next loop
				lastActivity = thisDate
			}

		} else {
			thisDate := temp_rows[i].StartTime
			prevDate := lastActivity
			prevIterDate := temp_rows[i-1].StartTime

			_, thisDateWeek := thisDate.ISOWeek() //adding day keeps i in the correct week
			_, prevDateWeek := prevDate.ISOWeek() // "

			if thisDateWeek != prevDateWeek || i == len(temp_rows)-1 { //if new week or last activity
				var summedWeeklyHbz Hbz
				var summedWeeklyPbz Pbz
				numResult++

				summedWeeklyHbz.AvZ1 = utility.Round((float64(sH1) / 3600.0), .5, 2)
				summedWeeklyHbz.AvZ2 = utility.Round((float64(sH2) / 3600.0), .5, 2)
				summedWeeklyHbz.AvZ3 = utility.Round((float64(sH3) / 3600.0), .5, 2)
				summedWeeklyHbz.AvZ4 = utility.Round((float64(sH4) / 3600.0), .5, 2)
				summedWeeklyHbz.AvZ5a = utility.Round((float64(sH5a) / 3600.0), .5, 2)
				summedWeeklyHbz.AvZ5b = utility.Round((float64(sH5b) / 3600.0), .5, 2)
				summedWeeklyHbz.AvZ5c = utility.Round((float64(sH5c) / 3600.0), .5, 2)

				summedWeeklyPbz.AvZ1 = utility.Round((float64(sP1) / 3600.0), .5, 2)
				summedWeeklyPbz.AvZ2 = utility.Round((float64(sP2) / 3600.0), .5, 2)
				summedWeeklyPbz.AvZ3 = utility.Round((float64(sP3) / 3600.0), .5, 2)
				summedWeeklyPbz.AvZ4 = utility.Round((float64(sP4) / 3600.0), .5, 2)
				summedWeeklyPbz.AvZ5 = utility.Round((float64(sP5) / 3600.0), .5, 2)
				summedWeeklyPbz.AvZ6 = utility.Round((float64(sP6) / 3600.0), .5, 2)

				monthS := prevDate.Month()
				dayS := strconv.Itoa(prevDate.Day())
				var dayF string

				var monthF time.Month
				if thisDateWeek != prevDateWeek {
					monthF = prevIterDate.Month()
					dayF = strconv.Itoa(prevIterDate.Day())
				} else {
					monthF = thisDate.Month()
					dayF = strconv.Itoa(thisDate.Day())
				}

				monthStrS := monthS.String()
				monthAbrS := monthStrS[0:3]
				monthStrF := monthF.String()
				monthAbrF := monthStrF[0:3]

				//format labels according to number to displau
				if filter.Historylen < 120 {
					summedWeeklyHbz.TimeLabel = dayS + " " + monthAbrS + " - " + dayF + " " + monthAbrF
					summedWeeklyPbz.TimeLabel = dayS + " " + monthAbrS + " - " + dayF + " " + monthAbrF
				} else {
					summedWeeklyHbz.TimeLabel = dayS + " " + monthAbrS
					summedWeeklyPbz.TimeLabel = dayS + " " + monthAbrS
				}

				clearVals()

				hbz_data = append(hbz_data, summedWeeklyHbz)
				pbz_data = append(pbz_data, summedWeeklyPbz)

				//reset the last activity date for the next loop
				lastActivity = thisDate

			}

		}
		sP1 += temp_rows[i].CountPZ1
		sP2 += temp_rows[i].CountPZ2
		sP3 += temp_rows[i].CountPZ3
		sP4 += temp_rows[i].CountPZ4
		sP5 += temp_rows[i].CountPZ5
		sP6 += temp_rows[i].CountPZ6

		sH1 += temp_rows[i].CountHZ1
		sH2 += temp_rows[i].CountHZ2
		sH3 += temp_rows[i].CountHZ3
		sH4 += temp_rows[i].CountHZ4
		sH5a += temp_rows[i].CountHZ5a
		sH5b += temp_rows[i].CountHZ5b
		sH5c += temp_rows[i].CountHZ5c
	}

	return hbz_data, pbz_data
}

//Heart vs Power AKA Performance chart!!!
func hvp(user types.UserSettings, filter Filter) []Hvp {
	user_id := user.Id
	hvp_data := make([]Hvp, 0)
	var has_heart, has_power bool
	var user_data types.Metrics
	var end_summary_json []byte
	var activity_start time.Time
	var cp_data_json []byte
	var user_cpms types.CPMs
	var activity_id string
	var lastNotableCp float64
	var thisCp int
	var thisAvHr int
	var thisAvCad int
	var standard_ride_id int
	standardRideSelection := false

	cluster := gocql.NewCluster(config.DbHost)
	cluster.Keyspace = "joulepersecond"
	cluster.Consistency = gocql.Quorum
	session, _ := cluster.CreateSession()
	defer session.Close()

	//get all of the user's data (at least all for now) TODO limit these queries by date if poss. Done!
	timeNow := time.Now().AddDate(0, 0, -filter.OffsetDays) //either now (0) or user specified offset (days)
	timeThen := timeNow.AddDate(0, 0, -filter.Historylen)
	iter := session.Query(`SELECT activity_id, activity_start, end_summary_json, has_power, has_heart FROM joulepersecond.user_activity WHERE user_id = ? AND activity_start > ? AND activity_start <= ? ORDER BY activity_start ASC`, user_id, timeThen, timeNow).Iter()
	for iter.Scan(&activity_id, &activity_start, &end_summary_json, &has_power, &has_heart) {
		iter := session.Query(`SELECT cp_data_json FROM joulepersecond.proc_activity WHERE activity_id = ? LIMIT 1`, activity_id).Iter()
		for iter.Scan(&cp_data_json) {
			json.Unmarshal(cp_data_json, &user_cpms)
		}
		if filter.S5 {
			thisCp = user_cpms.FiveSecondCP
			thisAvHr = user_cpms.FiveSecondCPHR
			thisAvCad = user_cpms.FiveSecondCPCAD
		} else if filter.S20 {
			thisCp = user_cpms.TwentySecondCP
			thisAvHr = user_cpms.TwentySecondCPHR
			thisAvCad = user_cpms.TwentySecondCPCAD
		} else if filter.S60 {
			thisCp = user_cpms.SixtySecondCP
			thisAvHr = user_cpms.SixtySecondCPHR
			thisAvCad = user_cpms.SixtySecondCPCAD
		} else if filter.S300 {
			thisCp = user_cpms.FiveMinuteCP
			thisAvHr = user_cpms.FiveMinuteCPHR
			thisAvCad = user_cpms.FiveMinuteCPCAD
		} else if filter.S1200 {
			thisCp = user_cpms.TwentyMinuteCP
			thisAvHr = user_cpms.TwentyMinuteCPHR
			thisAvCad = user_cpms.TwentyMinuteCPCAD
		} else if filter.S3600 {
			thisCp = user_cpms.SixtyMinuteCP
			thisAvHr = user_cpms.SixtyMinuteCPHR
			thisAvCad = user_cpms.SixtyMinuteCPCAD
		}

		var hvp_data_point Hvp
		var omitFromPC bool
		//unmarshal the activtiy metrics
		json.Unmarshal(end_summary_json, &user_data)

		//we will filter based on the results of this query
		session.Query(`SELECT activity_id, activity_name, is_indoor, is_outdoor, is_race, is_training, omit_from_pc, standard_ride_id FROM activity_meta WHERE activity_id = ?`, activity_id).Scan(
			&hvp_data_point.Meta.ActivityID, //even though we already have the id, we collect it here as a determinant of the presence acitivity meta data
			&hvp_data_point.Meta.ActivityName,
			&hvp_data_point.Meta.IndoorRide,
			&hvp_data_point.Meta.OutdoorRide,
			&hvp_data_point.Meta.Race,
			&hvp_data_point.Meta.Train,
			&omitFromPC,
			&standard_ride_id)

		//As there are no table joins we need to filter off unwanted activities through code...:
		for s := 0; s < len(filter.StandardRides); s++ {
			if filter.StandardRides[s] > 0 { //default
				standardRideSelection = true
			}
		}

		//if the user has made one or more selections discard as necessary for each that isn't in the array/list of standard rides
		if standardRideSelection {
			var conforms = false
			//in array?
			for t := 0; t < len(filter.StandardRides); t++ {
				if standard_ride_id == filter.StandardRides[t] {
					conforms = true
				}
			}
			if !conforms {
				continue //skip to next record
			}
		}

		var f1, f2, f3, f4 bool //filter flags

		//filter duration (f1)
		hvpFrom := time.Duration(filter.HvpFrom) * time.Minute
		hvpTo := time.Duration(filter.HvpTo) * time.Minute
		if user_data.Dur >= hvpFrom && user_data.Dur <= hvpTo {
			f1 = true
		} else {
			f1 = false
		}
		//if no filter was set
		if filter.HvpTo == 0 && filter.HvpTo == 0 {
			f1 = true
		}

		//filter indoor/outdoor and competitive/non-competitive (f2 and f3)
		f2 = true
		f3 = true
		f4 = true
		if hvp_data_point.Meta.ActivityID != "" { //only filter when there IS meta data - TODO add note to user on activity page that they need to add meta for the filter to work correctly - filters are only applied when values are supplied!!!!
			if filter.Indoor || filter.Outdoor { //filter on
				if filter.Indoor == true && filter.Outdoor == false {
					if hvp_data_point.Meta.OutdoorRide == true {
						f2 = false
					}
				}
				if filter.Indoor == false && filter.Outdoor == true {
					if hvp_data_point.Meta.IndoorRide == true {
						f2 = false
					}
				}
			} else { //no user filter
				f2 = false
			}
			if filter.Race || filter.Train { //filter on
				if filter.Race == true && filter.Train == false {
					if hvp_data_point.Meta.Train == true {
						f3 = false
					}
				}
				if filter.Race == false && filter.Train == true {
					if hvp_data_point.Meta.Race == true {
						f3 = false
					}
				}
			} else { //no user filter
				f3 = false
			}
			if omitFromPC {
				f4 = false
			}
		} //end of filter setup section

		if filter.HeartData && !has_heart {
			continue
		}

		if has_power && f1 && f2 && f3 && f4 { //apply filters
			//get the date in highcharts format
			activityDate := user_data.StartTime
			year, month, day := activityDate.Date()
			hvp_data_point.Year = year
			hvp_data_point.Month = int(month) - 1
			hvp_data_point.Day = day

			if float64(thisCp) > lastNotableCp {
				lastNotableCp = float64(thisCp)
				hvp_data_point.NotableCp = float64(thisCp)
				hvp_data_point.CpHr = thisAvHr
				hvp_data_point.CpCad = thisAvCad
				hvp_data_point.HasValue = true

			} else {
				lastNotableCp = lastNotableCp * float64(user.Ncp_rolloff) / 1000 //0.995 default
			}

			hvp_data_point.AvHeartRate = user_data.Avheart
			hvp_data_point.AvPower = user_data.Avpower
			hvp_data_point.AvCadence = user_data.Avcad
			hvp_data = append(hvp_data, hvp_data_point)
		}
	}
	return hvp_data
}

//Tss vs Duration
func tvd(user types.UserSettings, filter Filter) ([]Tvd, string) {
	user_id := user.Id
	tvd_data_points := make([]Tvd_data_point, 0)
	tvd_data := make([]Tvd, 0)
	var user_data types.Metrics
	var end_summary_json []byte
	var activity_start time.Time
	var tvdLegend string

	cluster := gocql.NewCluster(config.DbHost)
	cluster.Keyspace = "joulepersecond"
	cluster.Consistency = gocql.Quorum
	session, _ := cluster.CreateSession()
	defer session.Close()

	//get all of the user's data (at least all for now) TODO limit these queries by date if poss. Done!
	timeNow := time.Now()
	timeThen := timeNow.AddDate(0, 0, -filter.Historylen)
	iter := session.Query(`SELECT activity_start, end_summary_json FROM joulepersecond.user_activity WHERE user_id = ? AND activity_start > ? ORDER BY activity_start ASC`, user_id, timeThen).Iter()
	for iter.Scan(&activity_start, &end_summary_json) {
		var tvd_data_point Tvd_data_point
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
	}

	//we now have all the data... Now sort it
	sumTss := 0
	var sumDur time.Duration
	var lastActivity time.Time
	//loope through each retrieved activity
	for i := 1; i < len(tvd_data_points); i++ {
		//set last activity on first iteration only
		if i == 1 {
			lastActivity = tvd_data_points[i].Date
		}
		//if we want to show data in monthly format
		if filter.Historylen > 366 { //show over 365 days as monthly
			thisDate := tvd_data_points[i].Date
			prevDate := lastActivity

			//if we're still in the current month sum these values
			if thisDate.Month() != prevDate.Month() || i == len(tvd_data_points)-1 {
				var summedMonthlyTvd Tvd
				summedMonthlyTvd.TotalTss = sumTss
				summedMonthlyTvd.TotalDur = utility.Round(sumDur.Hours(), .5, 2)

				var month time.Month
				var year string
				if thisDate.Month() != prevDate.Month() {
					month = prevDate.Month()
					year = strconv.Itoa(prevDate.Year())
				} else {
					month = thisDate.Month()
					year = strconv.Itoa(thisDate.Year())
				}

				monthStr := month.String()
				summedMonthlyTvd.TimeLabel = monthStr[0:3] + " '" + year[2:4]

				sumTss = 0
				sumDur = 0
				tvd_data = append(tvd_data, summedMonthlyTvd)

				//reset the last activity date for the next loop
				lastActivity = thisDate
			}

			sumTss += tvd_data_points[i].Tss
			sumDur += tvd_data_points[i].Dur

			tvdLegend = "By Month"
		} else {
			thisDate := tvd_data_points[i].Date //we use this to compare the activity being scanned with the last to see if it is in the same week
			prevDate := lastActivity            //this is the last activity that we scanned that was the first of the new week, last week. Confusing init? we have to get the value now, and change if the weeks are not equal (new week)
			prevIterDate := tvd_data_points[i-1].Date

			//get week number for this and last activity
			_, thisDateWeek := thisDate.ISOWeek()
			prevDateYear, prevDateWeek := prevDate.ISOWeek()

			if thisDateWeek != prevDateWeek || i == len(tvd_data_points)-1 {
				var summedWeeklyTvd Tvd
				summedWeeklyTvd.TotalTss = sumTss
				summedWeeklyTvd.TotalDur = utility.Round(sumDur.Hours(), .5, 2)

				monthS := prevDate.Month()

				var monthF time.Month
				var dayF string

				if thisDateWeek != prevDateWeek {
					monthF = prevIterDate.Month()
					dayF = strconv.Itoa(prevIterDate.Day())

				} else {
					monthF = thisDate.Month()
					dayF = strconv.Itoa(thisDate.Day())
				}

				dayS := strconv.Itoa(prevDate.Day())

				monthStrS := monthS.String()
				monthAbrS := monthStrS[0:3]
				monthStrF := monthF.String()
				monthAbrF := monthStrF[0:3]

				//format labels according to number to displau
				if filter.Historylen < 120 {
					summedWeeklyTvd.TimeLabel = dayS + " " + monthAbrS + " - " + dayF + " " + monthAbrF
				} else {
					summedWeeklyTvd.TimeLabel = dayS + " " + monthAbrS
				}

				sumTss = 0
				sumDur = 0
				tvd_data = append(tvd_data, summedWeeklyTvd)

				//reset the last activity date for the next loop
				lastActivity = thisDate

			}

			//sum the values
			sumTss += tvd_data_points[i].Tss
			sumDur += tvd_data_points[i].Dur

			tvdLegend = "By week number: Series ending wk" + strconv.Itoa(prevDateWeek) + ", " + strconv.Itoa(prevDateYear)

		}
	}
	return tvd_data, tvdLegend
}

//fitness and freshness
func ff(user types.UserSettings, filter Filter) []types.Ff_data_point {
	user_id := user.Id
	ff_data := make([]types.Ff_data_point, 0)      //activity day
	ff_scan_data := make([]types.Ff_data_point, 0) //all days in range

	var user_data types.Metrics
	var user_cpms types.CPMs
	var end_summary_json []byte
	var cp_data_json []byte
	var has_power, has_heart bool
	var activity_start time.Time
	var firstDate time.Time
	var firstIter = true

	var activity_id string

	cluster := gocql.NewCluster(config.DbHost)
	cluster.Keyspace = "joulepersecond"
	cluster.Consistency = gocql.Quorum
	session, _ := cluster.CreateSession()
	defer session.Close()

	var lastNotableCp float64
	var thisCp int

	//get all of the user's data (at least all for now) TODO limit these queries by date if poss.
	iter := session.Query(`SELECT activity_start, activity_id, end_summary_json, has_power, has_heart FROM joulepersecond.user_activity WHERE user_id = ? ORDER BY activity_start ASC`, user_id).Iter()
	//loop through each activity
	for iter.Scan(&activity_start, &activity_id, &end_summary_json, &has_power, &has_heart) {
		iter := session.Query(`SELECT cp_data_json FROM joulepersecond.proc_activity WHERE activity_id = ? LIMIT 1`, activity_id).Iter()
		for iter.Scan(&cp_data_json) {
			json.Unmarshal(cp_data_json, &user_cpms)

		}
		if firstIter {
			firstDate = activity_start
			firstIter = false
		}
		var ff_data_point types.Ff_data_point
		//unmarshal the activtiy types.Metrics
		json.Unmarshal(end_summary_json, &user_data)

		//values for each scanned activity
		var user_tss int
		var perc_effort int
		var mot_level int

		//check for a tss override value
		session.Query(`SELECT activity_id, tss_value, motivation_level, perceived_effort FROM activity_meta WHERE activity_id = ?`, activity_id).Scan(&activity_id, &user_tss, &mot_level, &perc_effort)

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

		if filter.S5 {
			thisCp = user_cpms.FiveSecondCP
		} else if filter.S20 {
			thisCp = user_cpms.TwentySecondCP
		} else if filter.S60 {
			thisCp = user_cpms.SixtySecondCP
		} else if filter.S300 {
			thisCp = user_cpms.FiveMinuteCP
		} else if filter.S1200 {
			thisCp = user_cpms.TwentyMinuteCP
		} else if filter.S3600 {
			thisCp = user_cpms.SixtyMinuteCP
		}

		if float64(thisCp) > lastNotableCp {
			lastNotableCp = float64(thisCp)
			ff_data_point.NotableCp = float64(thisCp)

		} else {
			lastNotableCp = lastNotableCp * float64(user.Ncp_rolloff) / 1000 //0.995 default
		}

		//get the date
		ff_data_point.Date = user_data.StartTime

		//add extended metrics
		ff_data_point.Meta.MotivationLevel = mot_level
		ff_data_point.Meta.PerceivedEffort = perc_effort

		//add date and tss to array slice
		ff_data = append(ff_data, ff_data_point)

	}
	if err := iter.Close(); err != nil {
		log.Fatal(err)
	}
	//calulate the number of days between first recorded activity and current date
	duration := time.Since(firstDate) //get total duration in time.Duration
	days := int(duration/day) + 1     //add one day to include current day

	//loop through each day.
	for i := 0; i <= days+30; i++ { //add 30 days to show roll off
		//create a var to hold a sample for each day scanned - it may or maynot contain an activity, but will still contain ctl, atl and tsb vals along with the date
		var scan_data_point types.Ff_data_point

		//get each day to be scanned (all days between first activity and current date)
		scanDate := firstDate.AddDate(0, 0, i)
		scan_data_point.Date = scanDate                 //get the date
		scanYear, scanMonth, scanDay := scanDate.Date() //get date elements that are easier to compare

		//these so we can print out UTC values to Highcharts
		scan_data_point.Day = scanDay
		scan_data_point.Month = int(scanMonth) - 1 //js months start at naught.
		scan_data_point.Year = scanYear

		//now get each of the days where we have an activity
		for _, val := range ff_data {
			actYear, actMonth, actDay := val.Date.Date()
			if actYear == scanYear && actMonth == scanMonth && actDay == scanDay {
				scan_data_point.Meta.PerceivedEffort = val.Meta.PerceivedEffort
				scan_data_point.Meta.MotivationLevel = val.Meta.MotivationLevel
				//these are the days where we have activites - add the TSS

				//we might have more than one activity in a day so append any others TSS on this day
				scan_data_point.Tss += val.Tss
				if val.NotableCp > 0 {
					scan_data_point.NotableCp = val.NotableCp
					scan_data_point.HasValue = true
				} else {
					scan_data_point.HasValue = false
				}

			}
			//else leave TSS as it is (0)
		}
		if i > 0 { //don't have a yesterday value on the first day
			scan_data_point.Atl = ff_scan_data[i-1].Atl + (float64(scan_data_point.Tss)-ff_scan_data[i-1].Atl)/float64(user.Atl_constant)
			scan_data_point.Ctl = ff_scan_data[i-1].Ctl + (float64(scan_data_point.Tss)-ff_scan_data[i-1].Ctl)/float64(user.Ctl_constant)
			scan_data_point.Tsb = scan_data_point.Ctl - scan_data_point.Atl
			//fmt.Printf("Atl: %v Ctl: %v Tsb: %v\n", scan_data_point.Atl, scan_data_point.Ctl, scan_data_point.Tsb)
		}

		//add the data to our array slice
		ff_scan_data = append(ff_scan_data, scan_data_point)

	}
	//OPTION (show reduced results) commenting out the follwing lines bypasses filter
	if len(ff_scan_data) > (filter.Historylen + 30) {
		ff_scan_data = ff_scan_data[len(ff_scan_data)-(filter.Historylen+30) : len(ff_scan_data)]
	}

	return ff_scan_data
}

//combines the standard CpRow with the date of the sample
type CpMerged struct {
	CpRow   CpRow
	CpLabel string
	CpTime  int //time in seconds
}

type ByTimecode []CpMerged

func (a ByTimecode) Len() int           { return len(a) }
func (a ByTimecode) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByTimecode) Less(i, j int) bool { return a[i].CpTime < a[j].CpTime }

func powercurve(user types.UserSettings, filter Filter) ([]Cp3, Cp3Legend) {
	user_id := user.Id

	mergedCpRows := make([][]CpMerged, 0)

	var activity_id string

	var end_summary []byte
	var endSummary types.Metrics
	var power_series []byte
	var cp_row_json []byte
	var has_power bool
	var cpRows []CpRow

	powerSeries := make([]int, 0)

	cluster := gocql.NewCluster(config.DbHost)
	cluster.Keyspace = "joulepersecond"
	cluster.Consistency = gocql.Quorum
	session, _ := cluster.CreateSession()
	defer session.Close()

	timeNow := time.Now()
	timeThen := timeNow.AddDate(0, 0, -filter.Historylen*3)

	//get activities
	iter := session.Query(`SELECT activity_id FROM joulepersecond.user_activity WHERE user_id = ? AND activity_start > ? ORDER BY activity_start ASC`, user_id, timeThen).Iter()

	//********loop through each activity********
	var mergedCpRow_1 []CpMerged //to store merged cpRows retrieved from database
	var mergedCpRow_2 []CpMerged //to store merged cpRows retrieved from database
	var mergedCpRow_3 []CpMerged //to store merged cpRows retrieved from database

	for iter.Scan(&activity_id) {
		var merged CpMerged //to temporarily store each merged cpRow/label
		//var overwritten bool //if value has to be written if not an overwrite
		session.Query(`SELECT end_summary_json, power_json, cp_row_json, has_power FROM proc_activity WHERE activity_id = ?`, activity_id).Scan(&end_summary, &power_series, &cp_row_json, &has_power)
		json.Unmarshal(power_series, &powerSeries)
		json.Unmarshal(end_summary, &endSummary)
		json.Unmarshal(cp_row_json, &cpRows)

		//calculate age of activity for assignment to each quadrant
		activityAge := int((time.Since(endSummary.StartTime)) / day) //age of activity in days... Might need to add one(+1) to include full period

		//function which repeats for each data series (called three times)
		makeGraphData := func(mergedRow []CpMerged) []CpMerged {
			//loop through each of the activity rows
			for _, valFromJson := range cpRows {

				//add all leading zeros so that array e.g. 1,2,3 can be converted and sorted as integer eg. 10203
				hours := strconv.Itoa(valFromJson.CpTime[0])
				minutes := strconv.Itoa(valFromJson.CpTime[1])
				seconds := strconv.Itoa(valFromJson.CpTime[2])
				if len([]rune(hours)) == 1 {
					hours = "0" + hours
				} else if len([]rune(hours)) == 0 {
					hours = "00"
				}
				if len([]rune(minutes)) == 1 {
					minutes = "0" + minutes
				} else if len([]rune(minutes)) == 0 {
					minutes = "00"
				}
				if len([]rune(seconds)) == 1 {
					seconds = "0" + seconds
				} else if len([]rune(seconds)) == 0 {
					seconds = "00"
				}
				merged.CpTime = (valFromJson.CpTime[0] * 3600) + (valFromJson.CpTime[1] * 60) + valFromJson.CpTime[2] //hours minutes and seconds to seconds
				merged.CpRow.CpVal = valFromJson.CpVal
				merged.CpRow.CpTime = valFromJson.CpTime

				const longForm = "Mon&nbsp;Jan&nbsp;2,&nbsp;2006&nbsp;3:04pm"

				activityStart := endSummary.StartTime.Format(longForm)

				merged.CpLabel = hours + ":" + minutes + ":" + seconds + "&nbsp;-&nbsp;" + activityStart

				mergedRow = append(mergedRow, merged)

				//loop through each row and compare with all the other rows. when a match (many will) delete the lowest value When equal allow first value and then delete subsequent matches
				for i := 0; i < len(mergedRow); i++ {
					count := 0
					for j := 0; j < len(mergedRow); j++ {
						if mergedRow[i].CpRow.CpTime == mergedRow[j].CpRow.CpTime {
							if mergedRow[i].CpRow.CpVal > mergedRow[j].CpRow.CpVal {
								mergedRow = append(mergedRow[:j], mergedRow[j+1:]...)
								break
							} else if mergedRow[i].CpRow.CpVal < mergedRow[j].CpRow.CpVal {
								mergedRow = append(mergedRow[:i], mergedRow[i+1:]...)
								break
							} else {
								if count > 0 {
									mergedRow = append(mergedRow[:i], mergedRow[i+1:]...)
									break
								}
								count++
							}

						}
					}
				}
			}
			return mergedRow
		}

		//process each file within quantized time period - they don't need to be equal, they merely cause the data to be pushed to a new array/slice element
		//perhaps we could multi thread this?
		if activityAge < filter.Historylen && has_power {
			mergedCpRow_1 = makeGraphData(mergedCpRow_1)
		} else if activityAge >= filter.Historylen && activityAge < (filter.Historylen*2) && has_power {
			mergedCpRow_2 = makeGraphData(mergedCpRow_2)
		} else if activityAge >= (filter.Historylen*2) && activityAge < (filter.Historylen*3) && has_power {
			mergedCpRow_3 = makeGraphData(mergedCpRow_3)
		}

	} //end for each activity
	//fmt.Printf("%v\n\n", mergedCpRow)
	sort.Sort(ByTimecode(mergedCpRow_1))
	sort.Sort(ByTimecode(mergedCpRow_2))
	sort.Sort(ByTimecode(mergedCpRow_3))

	//function to merge the three date ranged structs into one
	mergeToLargest := func(largest, series2, series3 []CpMerged) []Cp3 {
		var dateRangedCpRow Cp3
		dateRangedCpRows := make([]Cp3, 0)
		//loop through largest file and copy required contents to new single struct
		for i := 0; i < len(largest); i++ {
			dateRangedCpRow.CpTime = largest[i].CpTime

			dateRangedCpRow.CpPower1 = largest[i].CpRow.CpVal
			dateRangedCpRow.CpLabel1 = largest[i].CpLabel

			if i < len(series2) {
				dateRangedCpRow.CpPower2 = series2[i].CpRow.CpVal
				dateRangedCpRow.CpLabel2 = series2[i].CpLabel
			} else {
				dateRangedCpRow.CpPower2 = 0
				dateRangedCpRow.CpLabel2 = ""
			}

			if i < len(series3) {
				dateRangedCpRow.CpPower3 = series3[i].CpRow.CpVal
				dateRangedCpRow.CpLabel3 = series3[i].CpLabel
			} else {
				dateRangedCpRow.CpPower3 = 0
				dateRangedCpRow.CpLabel3 = ""
			}
			dateRangedCpRows = append(dateRangedCpRows, dateRangedCpRow)

		}
		return dateRangedCpRows
	}
	var allCpData []Cp3

	var legends Cp3Legend

	s1 := "Last " + strconv.Itoa(filter.Historylen) + " Days"
	s2 := strconv.Itoa(filter.Historylen) + " to " + strconv.Itoa(filter.Historylen*2) + " Days ago"
	s3 := strconv.Itoa(filter.Historylen*2) + " to " + strconv.Itoa(filter.Historylen*3) + " Days ago"

	//find the largest data series TODO need further ordering to that legend corresponds to correct series
	if len(mergedCpRow_1) >= len(mergedCpRow_2) && len(mergedCpRow_1) >= len(mergedCpRow_3) {
		allCpData = mergeToLargest(mergedCpRow_1, mergedCpRow_2, mergedCpRow_3)
		legends.Series1 = s1
		legends.Series2 = s2
		legends.Series3 = s3
	} else if len(mergedCpRow_2) >= len(mergedCpRow_1) && len(mergedCpRow_2) >= len(mergedCpRow_3) {
		allCpData = mergeToLargest(mergedCpRow_2, mergedCpRow_1, mergedCpRow_3)
		legends.Series1 = s2
		legends.Series2 = s1
		legends.Series3 = s3
	} else if len(mergedCpRow_3) >= len(mergedCpRow_1) && len(mergedCpRow_3) >= len(mergedCpRow_2) {
		allCpData = mergeToLargest(mergedCpRow_3, mergedCpRow_1, mergedCpRow_2)
		legends.Series1 = s3
		legends.Series2 = s1
		legends.Series3 = s2
	}

	mergedCpRows = append(mergedCpRows, mergedCpRow_1)
	mergedCpRows = append(mergedCpRows, mergedCpRow_2)
	mergedCpRows = append(mergedCpRows, mergedCpRow_3)

	if err := iter.Close(); err != nil {
		log.Fatal(err)
	}

	return allCpData, legends
}

func view(user types.UserSettings, filter Filter) (p Page) {

	//get ff data - would be good not to get this on every activity call ********************
	var ffData []types.Ff_data_point
	if filter.ShowTss {
		ffData = ff(user, filter)
	}

	//power curve
	var cpData []Cp3
	var legends Cp3Legend
	if filter.ShowMmp {
		cpData, legends = powercurve(user, filter)
	}

	//Heart vs Power
	var hvpData []Hvp
	if filter.ShowHvp {
		hvpData = hvp(user, filter)
	}
	var hvp_label string
	if filter.HvpFrom == 0 && filter.HvpTo == 0 {
		hvp_label = "All ride durations"
	} else {
		hvp_label = "Rides with durations between <b>" + strconv.Itoa(filter.HvpFrom) + "</b> and <b>" + strconv.Itoa(filter.HvpTo) + "</b> minutes"
	}

	//Tss vs Duration
	var tvdData []Tvd
	var tvdLegend string
	if filter.ShowDur {
		tvdData, tvdLegend = tvd(user, filter)
	}

	//these two are well merged as a function so only don't process if both not required. else just hide the appropriate graphs

	var hbzData []Hbz
	var pbzData []Pbz
	if !filter.ShowHbz && !filter.ShowPbz {
		//these two are well merged as a function so only don't process if both not required. else just hide the appropriate graphs
	} else {
		hbzData, pbzData = hpbz(user, filter)
	}
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

	//cp data - doesn't actually need to be reversed (corrected), but just wanted to for future flexibility
	cpDataRev := make([]Cp3, 0)
	for i := len(cpData) - 1; i >= 0; i-- {
		cpDataRev = append(cpDataRev, cpData[i])
	}
	selectHTML := template.HTML("")

	for _, userval := range user.StandardRides { //for each of the user's standard rides
		selected := ""
		for _, filterval := range filter.StandardRides { //test for a filter match
			if userval.Id == filterval {
				selected = "selected"
			}
		}
		selectHTML += template.HTML("<option " + selected + " value=" + strconv.Itoa(userval.Id) + ">" + userval.Label + "</option>")
	}

	p = Page{
		FfData:            ffData,
		CpData:            cpDataRev,
		HvpData:           hvpData,
		TvdData:           tvdData,
		TvdLegend:         tvdLegend,
		CpLegend1:         legends.Series1,
		CpLegend2:         legends.Series2,
		CpLegend3:         legends.Series3,
		HvpLabel:          hvp_label,
		HbzData:           hbzData,
		PbzData:           pbzData,
		Settings:          user,
		Filter:            filter,
		ZoneLabels:        zoneLabels,
		StandardRidesHTML: selectHTML,
	}
	return
}
