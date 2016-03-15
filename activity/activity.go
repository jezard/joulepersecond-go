package activity

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gocql/gocql"
	"github.com/jezard/joulepersecond-go/conf"
	"github.com/jezard/joulepersecond-go/types"
	"github.com/jezard/joulepersecond-go/usersettings"
	"github.com/jezard/joulepersecond-go/utility"
	"html/template"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

//row of data struct
type SampleRow struct {
	Heartrate, Power, Cadence, Lapnumber int
	Lapstart, Timestamp                  time.Time
	NewTimestamp                         [3]int //in google chart timeofday format
}

/* Power Zones:
Zone 1 Less than 55% of FTPw
Zone 2 55% to 74% of FTPw
Zone 3 75% to 89% of FTPw
Zone 4 90% to 104% of FTPw
Zone 5 105% to 120% of FTPw
Zone 6 More than 120% of FTPw
*/

//data for Critical Power chart (single activity)
type CpRow struct {
	CpTime [3]int //in google chart timeofday format
	CpVal  int
	CpAhr  int
	CpAcad int
}

//store various types of information about an activity - 1 record per activity
type ActivityMeta struct {
	ActivityName, ActivityID                                      string
	TssOverride, MotivationLevel, PerceivedEffort, StandardRideId int
	IndoorRide, OutdoorRide, Race, Train                          bool
	OmitFromPC                                                    bool //omit ride from performance chart
}

//struct for html page template
type Page struct {
	Title        string
	ActivityMeta ActivityMeta
	Body         []byte
	Data         []SampleRow
	LapSummaries []types.Metrics
	EndSummary   types.Metrics
	CPM          types.CPMs //Critical power metrics (discrete measurements)
	CPData       []CpRow    //Time/value pairs for chart
	HasPower     bool
	HasHeart     bool
	HasCadence   bool
	ZoneData     types.Zones
	CurThr       int
	Theme        string
	Demo         bool
	Message      string
}

//create a data type to represent aggregated sample data
type Samples struct {
	Power, Hr, Cad, Samplecount, Freewheelcount int
}

var config = conf.Configuration()

func ActivityHandler(w http.ResponseWriter, r *http.Request) {

	// http://stackoverflow.com/questions/12830095/setting-http-headers-in-golang
	if origin := r.Header.Get("Origin"); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token")

	var meta ActivityMeta

	urlpath := r.URL.Path[1:] //get the path (after the domain:port slash)/eg view/activity/ActIviTyiD
	if urlpath == "favicon.ico" {
		http.NotFound(w, r)
	} else {
		urlparts := strings.Split(urlpath, "/")

		verb := urlparts[0] //eg view, process
		noun := urlparts[1] //eg activity, history

		switch verb {
		case "delete": //need to fix this with the new id system
			if noun == "activity" {
				cluster := gocql.NewCluster(config.DbHost)
				cluster.Keyspace = "joulepersecond"
				cluster.Consistency = gocql.Quorum
				session, _ := cluster.CreateSession()
				defer session.Close()

				activityId := urlparts[2] //get the encoded id
				access_token, err := url.QueryUnescape(urlparts[3])

				if err != nil {
					fmt.Printf(" Error: %v\n", err)
				}
				err = nil

				user, _ := Usersettings.Get(access_token)
				filename := urlparts[4]

				if user.Demo != false {
					url_err := errors.New("Forbidden: Cannot complete this task.")
					http.Error(w, url_err.Error(), http.StatusForbidden)
					return
				}

				//remove all traces of activity
				//deleting this on is a bit more tricky because of keys... so to get the start time of the acitivity, we'll use the first trackpoint timestamp, as this is equal to it and we can access that.
				var timestamp time.Time
				var lapstart time.Time
				if err := session.Query(`SELECT tp_timestamp, lap_start from joulepersecond.activity_data where activity_id = ? ORDER BY tp_timestamp ASC LIMIT 1`, activityId).Scan(&timestamp, &lapstart); err != nil {
					log.Printf("1: %v", err)
				}
				if err := session.Query(`DELETE FROM user_activity WHERE user_id = ? AND activity_start = ?`, user.Id, lapstart).Exec(); err != nil {
					log.Printf("2: %v", err)
				}
				//these are easier...
				if err := session.Query(`DELETE FROM activity_meta WHERE activity_id = ?`, activityId).Exec(); err != nil {
					log.Printf("3: %v", err)
				}
				if err := session.Query(`DELETE FROM proc_activity WHERE activity_id = ?`, activityId).Exec(); err != nil {
					log.Printf("4: %v", err)
				}
				if err := session.Query(`DELETE FROM activity_data WHERE activity_id = ?`, activityId).Exec(); err != nil {
					log.Printf("5: %v", err)
				}

				//delete the file
				err = os.Remove(config.UploadDir + filename)
				if err != nil {
					url_err := errors.New("The selected activity was not fully deleted.")
					http.Error(w, url_err.Error(), http.StatusInternalServerError)
					return
				}

			}
		case "process":
			if noun == "activity" {
				cluster := gocql.NewCluster(config.DbHost)
				cluster.Keyspace = "joulepersecond"
				cluster.Consistency = gocql.Quorum
				session, _ := cluster.CreateSession()
				defer session.Close()

				access_token, _ := url.QueryUnescape(urlparts[3])
				user, _ := Usersettings.Get(access_token)

				activityId := urlparts[2] //get the encoded id

				//add the meta data from settings
				if err := session.Query(`INSERT INTO activity_meta (activity_id, activity_ftp, activity_weight, activity_thr, activity_vo2 ) VALUES (?, ?, ?, ?, ?)`,
					activityId, user.Ftp, user.Weight, user.Thr, user.Vo2).Exec(); err != nil {
					log.Printf("Location:%v", err)
				}
				processActivity(activityId, user)
			}
			if noun == "file" {
				//this is a cURL request
				fileId := urlparts[2] //gets a pipe seperated directory structure
				processCQLfile(fileId)
			}
			break
		case "view":

			access_token, err := url.QueryUnescape(urlparts[3])

			if err != nil {
				fmt.Printf(" Error: %v\n", err)
			}
			err = nil

			user, err := Usersettings.Get(access_token)

			theme, err := r.Cookie("theme")
			if err != nil {
				user.Theme = "gray"
			} else {
				user.Theme = theme.Value
			}

			if noun == "activity" {
				cluster := gocql.NewCluster(config.DbHost)
				cluster.Keyspace = "joulepersecond"
				cluster.Consistency = gocql.Quorum
				session, _ := cluster.CreateSession()
				defer session.Close()

				activityId := urlparts[2] //get the encoded id

				//get the user set options for the activity...
				manualTss, err := strconv.Atoi(r.FormValue("tss"))
				if err == nil && user.Demo == false {
					meta.TssOverride = manualTss
					//add/update the database with the new values
					if err := session.Query(`INSERT INTO activity_meta (activity_id, tss_value ) VALUES (?, ?)`,
						activityId, meta.TssOverride).Exec(); err != nil {
						log.Printf("Location:%v", err)
					}
				}

				motivationLevel, err := strconv.Atoi(r.FormValue("motivation_level"))
				if err == nil && user.Demo == false {
					meta.MotivationLevel = motivationLevel
					//add/update the database with the new values
					if err := session.Query(`INSERT INTO activity_meta (activity_id, motivation_level ) VALUES (?, ?)`,
						activityId, meta.MotivationLevel).Exec(); err != nil {
						log.Printf("Location:%v", err)
					}
				}

				perceivedEffort, err := strconv.Atoi(r.FormValue("perceived_effort"))
				if err == nil && user.Demo == false {
					meta.PerceivedEffort = perceivedEffort
					//add/update the database with the new values
					if err := session.Query(`INSERT INTO activity_meta (activity_id, perceived_effort ) VALUES (?, ?)`,
						activityId, meta.PerceivedEffort).Exec(); err != nil {
						log.Printf("Location:%v", err)
					}
				}

				//standard ride id
				standardRideId, err := strconv.Atoi(r.FormValue("standard_ride_id"))

				if standardRideId != 0 && user.Demo == false {
					meta.StandardRideId = standardRideId
					//add/update the database with the new values
					if err := session.Query(`INSERT INTO activity_meta (activity_id, standard_ride_id ) VALUES (?, ?)`,
						activityId, meta.StandardRideId).Exec(); err != nil {
						log.Printf("Location:%v", err)
					}
				}

				//indoors or outdoors?
				inOrOut := r.FormValue("in_or_out")
				if inOrOut != "" && user.Demo == false {
					if inOrOut == "in" {
						meta.IndoorRide = true
						meta.OutdoorRide = false

					}
					if inOrOut == "out" {
						meta.IndoorRide = false
						meta.OutdoorRide = true
					}
					//add/update the database with the new values
					if err := session.Query(`INSERT INTO activity_meta (activity_id, is_indoor, is_outdoor ) VALUES (?, ?, ?)`,
						activityId, meta.IndoorRide, meta.OutdoorRide).Exec(); err != nil {
						log.Printf("Location:%v", err)
					}
				}

				//training or racing?
				trainOrRace := r.FormValue("race_or_train")
				if trainOrRace != "" && user.Demo == false {
					if trainOrRace == "race" {
						meta.Race = true
						meta.Train = false

					}
					if trainOrRace == "train" {
						meta.Race = false
						meta.Train = true
					}
					//add/update the database with the new values
					if err := session.Query(`INSERT INTO activity_meta (activity_id, is_race, is_training ) VALUES (?, ?, ?)`,
						activityId, meta.Race, meta.Train).Exec(); err != nil {
						log.Printf("Location:%v", err)
					}
				}

				//can we get the activity name?
				activityName := r.FormValue("activity_name")
				if activityName != "" && user.Demo == false {
					meta.ActivityName = activityName
					if err := session.Query(`INSERT INTO activity_meta (activity_id, activity_name ) VALUES (?, ?)`,
						activityId, meta.ActivityName).Exec(); err != nil {
						log.Printf("Location:%v", err)
					}
				}

				///omit from performance chart?
				if user.Demo == false {
					if r.Method == "POST" {
						omitFromPC := r.FormValue("omit-from-pc")
						if omitFromPC == "1" {
							meta.OmitFromPC = true
						} else {
							meta.OmitFromPC = false
						}

						//add/update the database with the new values
						if err := session.Query(`INSERT INTO activity_meta (activity_id, omit_from_pc ) VALUES (?, ?)`,
							activityId, meta.OmitFromPC).Exec(); err != nil {
							log.Printf("Location:%v", err)
						}
					}
				}

				//get any stored tss value for the activity
				if err := session.Query(`SELECT activity_id, activity_name, is_indoor, is_outdoor, is_race, is_training, tss_value, motivation_level, perceived_effort, omit_from_pc FROM activity_meta WHERE activity_id = ?`, activityId).Scan(&activityId, &meta.ActivityName, &meta.IndoorRide, &meta.OutdoorRide, &meta.Race, &meta.Train, &meta.TssOverride, &meta.MotivationLevel, &meta.PerceivedEffort, &meta.OmitFromPC); err != nil {
					//set user tss to 0 if no value held in db for activity
					meta.TssOverride = 0
				}

				p := viewActivity(activityId, user, meta)
				t, _ := template.ParseFiles(config.Tpath + "activity.html")
				t.Execute(w, p)
			}
			if noun == "range" {
				message := utility.Dostuff()
				fmt.Printf("%v", message)
				aggregate()
			}
		}
	}
}

func processCQLfile(filename string) {
	//restore te proper seperators to the file name
	filename = strings.Replace(filename, "|", "/", -1)

	cluster := gocql.NewCluster(config.DbHost)
	cluster.Keyspace = "joulepersecond"
	cluster.Consistency = gocql.Quorum
	session, _ := cluster.CreateSession()
	defer session.Close()

	file, err := os.Open(filename)
	if err != nil {
		log.Printf("Location:%v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		cql := scanner.Text()
		//insert each line of the file
		if err := session.Query(cql).Exec(); err != nil {
			log.Printf("Location:%v", err)
		}
	}
}

func getResults(activityId string) []map[string]interface{} {
	cluster := gocql.NewCluster(config.DbHost)
	cluster.Keyspace = "joulepersecond"
	cluster.Consistency = gocql.Quorum
	session, _ := cluster.CreateSession()
	defer session.Close()

	iter := session.Query(`SELECT * FROM activity_data WHERE activity_id = ?`, activityId).Iter()

	alldata, _ := iter.SliceMap()

	if err := iter.Close(); err != nil {
		log.Printf("Location:%v", err)
	}
	return alldata
}
func saveProcessed(user types.UserSettings, activityId, title string, row_json, power_json, heart_json, cadence_json, cp_row_json, cp_data_json, lap_summaries_json, end_summary_json []byte, hasPower, hasHeart, hasCadence bool, curFtp, curThr int, activityStart time.Time) {
	cluster := gocql.NewCluster(config.DbHost)
	cluster.Keyspace = "joulepersecond"
	cluster.Consistency = gocql.Quorum
	session, _ := cluster.CreateSession()
	defer session.Close()

	if err := session.Query(`INSERT INTO proc_activity (activity_id, title, row_json, power_json, heart_json, cadence_json, cp_row_json, cp_data_json, lap_summaries_json, end_summary_json, has_power, has_heart, has_cadence, cur_ftp, cur_thr) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		activityId, title, row_json, power_json, heart_json, cadence_json, cp_row_json, cp_data_json, lap_summaries_json, end_summary_json, hasPower, hasHeart, hasCadence, curFtp, curThr).Exec(); err != nil {
		log.Printf("Location:%v", err)
	}
	if err := session.Query(`INSERT INTO user_activity (user_id, activity_id, activity_start, end_summary_json, has_power, has_heart) VALUES (?, ?, ?, ?, ?, ?)`,
		user.Id, activityId, activityStart, end_summary_json, hasPower, hasHeart).Exec(); err != nil {
		log.Printf("Location:%v", err)
	}

}
func getPreProcessed(activityId string) (title string, row_json, power_json, heart_json, cp_row_json, cp_data_json, lap_summaries_json, end_summary_json []byte, has_power, has_heart, has_cadence bool, cur_ftp, cur_thr int) {
	cluster := gocql.NewCluster(config.DbHost)
	cluster.Keyspace = "joulepersecond"
	cluster.Consistency = gocql.Quorum
	session, _ := cluster.CreateSession()
	defer session.Close()

	if err := session.Query(`SELECT title, row_json, power_json, heart_json, cp_row_json, cp_data_json, lap_summaries_json, end_summary_json, has_power, has_heart, has_cadence, cur_ftp, cur_thr FROM proc_activity WHERE activity_id = ?`, activityId).Scan(&title, &row_json, &power_json, &heart_json, &cp_row_json, &cp_data_json, &lap_summaries_json, &end_summary_json, &has_power, &has_heart, &has_cadence, &cur_ftp, &cur_thr); err != nil {
		return title, row_json, power_json, heart_json, cp_row_json, cp_data_json, lap_summaries_json, end_summary_json, has_power, has_heart, has_cadence, cur_ftp, cur_thr
	}
	return title, row_json, power_json, heart_json, cp_row_json, cp_data_json, lap_summaries_json, end_summary_json, has_power, has_heart, has_cadence, cur_ftp, cur_thr

}

func processActivity(activityId string, user types.UserSettings) {
	//define one second
	second := time.Second
	//data is now of type []map[string]interface {}
	data := getResults(activityId)

	//vars to hold a single data row (instances of our struct types)
	var row SampleRow
	var lapSummary types.Metrics
	var endSummary types.Metrics

	//slices to hold mulitple rows
	rows := make([]SampleRow, 0)
	//and lap summary data
	lapSummaries := make([]types.Metrics, 0)
	//cp data
	cpRows := make([]CpRow, 0)

	//init vars
	var activityStart time.Time      //first lap start time
	var activity Samples             //aggregated activity samples
	var lap Samples                  //aggregated lap samples
	var laptime time.Time            //last interation's lap time marker
	var sampletime time.Time         //last iteration's sample time
	var sampleDistance time.Duration //time duration between this and last iteration's sample time
	//var timeSubtract time.Duration   //total time to subtract from sample time to give continuous line when using continuous axes
	var pedalcount int //temporary var storing number of samples with cadence value of 0
	var ElapsedTime time.Duration
	hasPower := true
	hasHeart := true
	hasCadence := true

	powerSeries := make([]int, 0)   //power time series data
	heartSeries := make([]int, 0)   //heart rate time series data
	cadenceSeries := make([]int, 0) //cadence time series data

	//see http://golang.org/pkg/time/#example_Parse
	const layout = "15:04:05"
	for _, val := range data {
		//val is now of type map[string]interface {}
		//e.g. val["tp_heartrate"] is type interface{}, but these values need to be asserted to their correct type for use in calculations etc.
		row.Heartrate = val["tp_heartrate"].(int)
		row.Power = val["tp_watts"].(int)
		row.Cadence = val["tp_cadence"].(int)
		row.Lapnumber = val["lap_number"].(int)
		row.Lapstart = val["lap_start"].(time.Time)
		//set the activity start time to that of the first lap
		if activityStart.IsZero() {
			activityStart = row.Lapstart
		}
		row.Timestamp = val["tp_timestamp"].(time.Time) //remember 'e.g. timestamp'.sub('e.g. lapstart') returns type time.Duration
		//subtract last sample time from this sample time to give a remainder duration
		sampleDistance = row.Timestamp.Sub(sampletime)

		//function summing metrics, repeated for each 'missing data point'
		sum := func() {
			//sum some of the metrics for averaging
			activity.Power += row.Power
			activity.Hr += row.Heartrate
			activity.Cad += row.Cadence
			//don't add to the average cadence val when freewheeling
			if activity.Cad == 0 {
				activity.Freewheelcount++
			}
			activity.Samplecount++
			//and for each lap too...
			lap.Power += row.Power
			lap.Hr += row.Heartrate
			lap.Cad += row.Cadence
			//don't add to the average cadence val when freewheeling
			if row.Cadence == 0 {
				lap.Freewheelcount++
				activity.Freewheelcount++
			}
			lap.Samplecount++
		}
		setZero := func() {
			//sum some of the metrics for averaging
			row.Power = 0
			row.Heartrate = 0
			row.Cadence = 0
			lap.Freewheelcount++
			activity.Freewheelcount++
			activity.Samplecount++
			lap.Samplecount++
		}

		//if time period is set and is greater than n seconds
		if ((sampleDistance / second) > user.Stopgap) && !(sampletime.IsZero()) {
			//check for error
			//fmt.Printf("Seconds: %v \n", sampleDistance)

		} else if ((sampleDistance / second) < user.Stopgap) && !(sampletime.IsZero()) {
			/**
			* Add samples / and missing samples if required by user (up until user defined stop duration)
			**/

			//define one second duration
			second := time.Second
			//get number of seconds between samples (all based on 1hz sample frequency)
			seconds := int(sampleDistance / second)

			for i := 0; i < seconds; i++ {
				//only add samples up until the user defined number of seconds
				if i < int(user.Stopgap) {
					//fmt.Printf("adding second %d\n", i)

					//add the missing samples
					if user.Autofill == "autofill" {
						sum()
					} else if user.Autofill == "setzero" {
						//only zero for more than 1 second else we'd never have any data!
						if seconds > 1 {
							setZero()
						} else {
							sum()
						}
					}

					//add a second for each second past, but not if autofill set to 'remove'
					if (user.Autofill == "autofill" || user.Autofill == "setzero") || (user.Autofill == "remove" && seconds == 1) {
						ElapsedTime += second
						row.NewTimestamp[0] = int(ElapsedTime.Hours())
						row.NewTimestamp[1] = int(ElapsedTime.Minutes()) % 60
						row.NewTimestamp[2] = int(ElapsedTime.Seconds()) % 60

						if user.Autofill == "remove" && seconds == 1 {
							sum()
						}

						//add this row's data to the slice
						rows = append(rows, row)
						//add the power value to the power time series
						powerSeries = append(powerSeries, row.Power)
						heartSeries = append(heartSeries, row.Heartrate)
						cadenceSeries = append(cadenceSeries, row.Cadence)
					}
				}
			}
		}

		sampletime = row.Timestamp //this might need to move position

		//if a new lap
		if row.Lapstart != laptime && lap.Samplecount > 0 && !(laptime.IsZero()) {
			//calculate lap totals
			lapSummary.Avpower = lap.Power / lap.Samplecount
			lapSummary.Avheart = lap.Hr / lap.Samplecount
			lapSummary.Dur = time.Duration(lap.Samplecount) * time.Second
			pedalcount = (lap.Samplecount - lap.Freewheelcount)
			if pedalcount > 0 {
				lapSummary.Avcad = lap.Cad / (lap.Samplecount - lap.Freewheelcount)
			} else {
				lapSummary.Avcad = 0
			}

			//append the summary lap data
			lapSummaries = append(lapSummaries, lapSummary)

			//reset lap
			laptime = row.Lapstart
			lap.Samplecount = 0
			lap.Freewheelcount = 0
			lap.Power = 0
			lap.Hr = 0
			lap.Cad = 0
		}
		if laptime.IsZero() {
			laptime = row.Lapstart
		}
	}

	//get the number of samples (these are already processed and are at one second intervals)
	seriesLen := len(rows)                                               //***would be good to save this data in cassandra***
	activityDuration := (time.Duration(seriesLen) * time.Second).Hours() //and this

	/***
	* Critical power
	***/
	var cpms types.CPMs
	var cpRow CpRow
	var maxCpVal int
	var maxCpHrVal int
	var maxCpCadVal int
	var sumCpVal int
	var sumHrVal int
	var sumCadVal int
	const accuracyVal = 2.25 //controls the accuracy of the output (l is more accurate [more sample points])
	var isPreset bool

	//make a set of preset timecodes/snapshot times - accuracyVal should not be changed once in production!
	presets := make([]int, 0)
	for i := 0; i < seriesLen; i++ {
		preset := int(math.Pow(float64(i), accuracyVal))

		if preset <= seriesLen {
			presets = append(presets, preset)
		}
	}
	//set initial val
	logVal := math.Log(float64(seriesLen)) //TODO remove this - not required/used?

	//this loop determines the length of the rolling sampling period to average (from the length of the activity to 1 second)
	for i := seriesLen; i > 0; i-- { //3600, 3599, 3598...=
		//check it this is one of our snapshots
		for _, presetTime := range presets {
			if i == presetTime {
				isPreset = true
				break
			}
			isPreset = false
		}

		//don't calculate ALL samples
		if isPreset || i == 1 || i == 2 || i == 3 || i == 4 || i == 5 || i == 10 || i == 20 || i == 30 || i == 60 || i == 5*60 || i == 20*60 || i == 30*60 || i == 60*60 || i == 120*60 || i == 240*60 || i == 360*60 || i == 480*60 || i == 600*60 {
			//reset max for each duration calculated
			maxCpVal = 0
			//this loop determines the point at which to start searching
			for j := 0; j <= (seriesLen - i); j++ { ///j=0; j<1; j++ ... j=1; j<2; j++ etc
				//rolling power slice is from start pos to smapling period
				rollingPowerSlice := powerSeries[j : j+i] // eg [1:2], [2:3], [3:4] etc...
				rollingHeartSlice := heartSeries[j : j+i]
				rollingCadenceSlice := cadenceSeries[j : j+i]

				//reset sum of slice vals
				sumCpVal = 0
				sumHrVal = 0
				sumCadVal = 0
				for _, val := range rollingPowerSlice {
					//sum the sliding slice values
					sumCpVal += val
				}
				for _, val := range rollingHeartSlice {
					//sum the sliding slice values
					sumHrVal += val
				}
				for _, val := range rollingCadenceSlice {
					//sum the sliding slice values
					sumCadVal += val
				}
				if (sumCpVal / i) > maxCpVal {
					maxCpVal = (sumCpVal / i)
					maxCpHrVal = (sumHrVal / i) //calucate the averate heart rate that accompanies the high critical power
					maxCpCadVal = (sumCadVal / i)
				}
			}
			//preset duration vals
			switch i {
			case 5:
				cpms.FiveSecondCP = maxCpVal
				cpms.FiveSecondCPHR = maxCpHrVal
				cpms.FiveSecondCPCAD = maxCpCadVal
				break
			case 20:
				cpms.TwentySecondCP = maxCpVal
				cpms.TwentySecondCPHR = maxCpHrVal
				cpms.TwentySecondCPCAD = maxCpCadVal
				break
			case 60:
				cpms.SixtySecondCP = maxCpVal
				cpms.SixtySecondCPHR = maxCpHrVal
				cpms.SixtySecondCPCAD = maxCpCadVal
				break
			case 300:
				cpms.FiveMinuteCP = maxCpVal
				cpms.FiveMinuteCPHR = maxCpHrVal
				cpms.FiveMinuteCPCAD = maxCpCadVal
				break
			case 1200:
				cpms.TwentyMinuteCP = maxCpVal
				cpms.TwentyMinuteCPHR = maxCpHrVal
				cpms.TwentyMinuteCPCAD = maxCpCadVal
				break
			case 3600:
				cpms.SixtyMinuteCP = maxCpVal
				cpms.SixtyMinuteCPHR = maxCpHrVal
				cpms.SixtyMinuteCPCAD = maxCpCadVal
				break
			}
			//should be able to do a plot from this info
			//fmt.Printf("Max Mean power for %d seconds is %d Watts. Log of i: %v\n", i, maxCpVal, math.Log(float64(i)))
			//plotinfo
			ElapsedTime = time.Duration(i) * time.Second //convert iterator (seconds) to Duration type
			cpRow.CpTime[0] = int(ElapsedTime.Hours())
			cpRow.CpTime[1] = int(ElapsedTime.Minutes()) % 60
			cpRow.CpTime[2] = int(ElapsedTime.Seconds()) % 60
			cpRow.CpVal = maxCpVal
			cpRow.CpAhr = maxCpHrVal
			cpRow.CpAcad = maxCpCadVal
			cpRows = append(cpRows, cpRow)

			logVal -= accuracyVal

		}

	}

	//post loop calculations
	if activity.Samplecount > 0 {
		//calculate lap totals
		lapSummary.Avpower = lap.Power / lap.Samplecount
		lapSummary.Avheart = lap.Hr / lap.Samplecount
		lapSummary.Dur = time.Duration(lap.Samplecount) * time.Second
		pedalcount = (lap.Samplecount - lap.Freewheelcount)
		if pedalcount > 0 {
			lapSummary.Avcad = lap.Cad / (lap.Samplecount - lap.Freewheelcount)
		} else {
			lapSummary.Avcad = 0
		}

		//append the summary lap data
		lapSummaries = append(lapSummaries, lapSummary)

		//calculate totals
		endSummary.Avpower = activity.Power / activity.Samplecount
		endSummary.Avheart = activity.Hr / activity.Samplecount
		pedalcount = (activity.Samplecount - activity.Freewheelcount)
		if pedalcount > 0 {
			endSummary.Avcad = activity.Cad / (activity.Samplecount - activity.Freewheelcount)
		} else {
			endSummary.Avcad = 0
		}
	}

	/***
	* normalised power
	***/

	var fourthPower float64
	var thirtySecondSum int
	var thirtySecondAv float64
	for i := 30; i < seriesLen; i++ {
		//reset total
		thirtySecondSum = 0
		//get thirty second rolling slice
		rollingPowerSlice := powerSeries[i-30 : i]
		for _, val := range rollingPowerSlice {
			//sum the sliding slice values
			thirtySecondSum += val
		}
		thirtySecondAv = float64(thirtySecondSum / 30)
		//multply by the power of 4
		fourthPower += math.Pow(thirtySecondAv, 4)
	}
	//normalised power = 4th root of total of 30 second averages divided my number of averages taken (total - 30 to allow for start offset and slice length)
	normalisedPower := int(math.Pow(fourthPower/float64(seriesLen-30), 0.25)) //4th root is power 1/4 (0.25)
	endSummary.Np = normalisedPower

	/***
	* Intensity factor
	***/

	intensity := float64(normalisedPower) / float64(user.Ftp)
	endSummary.If = utility.Round(intensity, .5, 2) * 100 //times by 100 and use as a percentage to avoid troubles

	/***
	* Intensity factor from heart rate (as a percentage)
	***/
	maxHr := float64(user.Thr) * 1.06
	endSummary.IfHr = int((float64(endSummary.Avheart) / maxHr) * 100)

	/***
	* TSS
	***/
	endSummary.Tss = int((float64(seriesLen) * float64(normalisedPower) * intensity) / (float64(user.Ftp) * 3600) * 100)

	/***
	* Estimated TSS
	***/
	var etssSum int
	var inc int
	for _, val := range heartSeries {
		if val == 0 {
			inc = 0 //no hr data
		} else if float64(val) < 0.81*float64(user.Thr) {
			inc = 55 //zone 1
		} else if float64(val) > 0.81*float64(user.Thr) && float64(val) <= 0.89*float64(user.Thr) {
			inc = 60 //zone 2
		} else if float64(val) > 0.89*float64(user.Thr) && float64(val) <= 0.93*float64(user.Thr) {
			inc = 69 //zone 3
		} else if float64(val) > 0.93*float64(user.Thr) && float64(val) <= 0.99*float64(user.Thr) {
			inc = 87 //zone 4
		} else if float64(val) > 0.99*float64(user.Thr) && float64(val) <= 1.02*float64(user.Thr) {
			inc = 100 //zone 5a
		} else if float64(val) > 1.02*float64(user.Thr) && float64(val) <= 1.06*float64(user.Thr) {
			inc = 118 //zone 5b
		} else if float64(val) > 1.06*float64(user.Thr) {
			inc = 140 //zone 5c
		}
		etssSum += inc
	}
	if len(heartSeries) > 0 {
		endSummary.Etss = int(float64(etssSum/len(heartSeries)) * activityDuration)
	}

	//set page var stuff

	if endSummary.Avpower == 0 {
		hasPower = false
	}
	if endSummary.Avheart == 0 {
		hasHeart = false
	}
	if endSummary.Avcad == 0 {
		hasCadence = false
	}

	/***
	* Energy
	***/

	if endSummary.Avpower > 0 {
		endSummary.WorkDone = int(float64(endSummary.Avpower)*float64(seriesLen)) / 1000 //KJ
		endSummary.EnergyUsedKj = int(float64(endSummary.WorkDone) * 4.444444444)        //KJoules 0.4444' is 1/22.5 (22.5% efficiency)
		endSummary.EnergyUsedKc = int(float64(endSummary.EnergyUsedKj) / 4.186)          //KCals 4.186 convert KJoules -> KCals
	} else if endSummary.Avheart > 0 && endSummary.Avpower == 0 {
		//if not power present, but vo2 Max is, use that instead (http://www.shapesense.com/fitness-exercise/calculators/heart-rate-based-calorie-burn-calculator.aspx)
		if user.Vo2 > 0 && user.Gender == "male" {
			endSummary.EnergyUsedKc = int(((-95.7735 + (0.634 * float64(endSummary.Avheart)) + (0.404 * float64(user.Vo2)) + (0.394 * float64(user.Weight)) + (0.271 * float64(user.Age))) / 4.184) * 60.00 * (float64(seriesLen) / 3600.00))
			endSummary.EnergyUsedKj = int(float64(endSummary.EnergyUsedKc) * 4.186)
			endSummary.WorkDone = int(float64(endSummary.EnergyUsedKj) * 0.225)
		} else if user.Vo2 > 0 && user.Gender == "female" {
			endSummary.EnergyUsedKc = int(((-59.3954 + (0.45 * float64(endSummary.Avheart)) + (0.380 * float64(user.Vo2)) + (0.103 * float64(user.Weight)) + (0.274 * float64(user.Age))) / 4.184) * 60.00 * (float64(seriesLen) / 3600.00))
			endSummary.EnergyUsedKj = int(float64(endSummary.EnergyUsedKc) * 4.186)
			endSummary.WorkDone = int(float64(endSummary.EnergyUsedKj) * 0.225)
		}
		//if user.Vo2 Max not known, but we have hr data
		if user.Vo2 == 0 && user.Gender == "male" {
			endSummary.EnergyUsedKc = int(((-55.0969 + (0.6309 * float64(endSummary.Avheart)) + (0.1988 * float64(user.Weight)) + (0.2017 * float64(user.Age))) / 4.184) * 60 * (float64(seriesLen) / 3600.00))
			endSummary.EnergyUsedKj = int(float64(endSummary.EnergyUsedKc) * 4.186)
			endSummary.WorkDone = int(float64(endSummary.EnergyUsedKj) * 0.225)
		} else if user.Vo2 == 0 && user.Gender == "female" {
			endSummary.EnergyUsedKc = int(((-20.4022 + (0.4472 * float64(endSummary.Avheart)) + (0.1263 * float64(user.Weight)) + (0.074 * float64(user.Age))) / 4.184) * 60 * (float64(seriesLen) / 3600.00))
			endSummary.EnergyUsedKj = int(float64(endSummary.EnergyUsedKc) * 4.186)
			endSummary.WorkDone = int(float64(endSummary.EnergyUsedKj) * 0.225)
		}
	}

	const longForm = "Monday Jan 2, 2006 at 3:04pm"
	const shortForm = "2006, 0, 2"
	var title = activityStart.Format(longForm)

	endSummary.Dur = time.Duration(seriesLen) * time.Second
	endSummary.StartTime = activityStart

	//**** Marshal JSON and save to Cassandra ********//
	row_json, err := json.Marshal(rows)              //raw processed data
	power_json, err := json.Marshal(powerSeries)     //for chart
	heart_json, err := json.Marshal(heartSeries)     //for chart
	cadence_json, err := json.Marshal(cadenceSeries) //for chart
	cp_row_json, err := json.Marshal(cpRows)         //rows for chart
	cp_data_json, err := json.Marshal(cpms)          //critical power metrics
	lap_summaries_json, err := json.Marshal(lapSummaries)
	end_summary_json, err := json.Marshal(endSummary)
	if err != nil {
		fmt.Println("error:", err)
	}
	saveProcessed(user, activityId, title, row_json, power_json, heart_json, cadence_json, cp_row_json, cp_data_json, lap_summaries_json, end_summary_json, hasPower, hasHeart, hasCadence, user.Ftp, user.Thr, activityStart)
}

func aggregate() {

}

func viewActivity(activityId string, user types.UserSettings, meta ActivityMeta) (p Page) {
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

	var endSummary types.Metrics
	var cpms types.CPMs
	var zoneData types.Zones

	//slices to hold mulitple rows
	rows := make([]SampleRow, 0)
	//and lap summary data
	lapSummaries := make([]types.Metrics, 0)

	//cp data
	cpRows := make([]CpRow, 0)

	hasPower := true
	hasHeart := true
	hasCadence := true

	powerSeries := make([]int, 0) //power time series data
	heartSeries := make([]int, 0) //heart rate time series data

	title, row_json, power_json, heart_json, cp_row_json, cp_data_json, lap_summaries_json, end_summary_json, has_power, has_heart, has_cadence, cur_ftp, cur_thr := getPreProcessed(activityId)

	json.Unmarshal(row_json, &rows)
	json.Unmarshal(power_json, &powerSeries) //need this?
	json.Unmarshal(heart_json, &heartSeries) //need this?
	json.Unmarshal(cp_row_json, &cpRows)
	json.Unmarshal(cp_data_json, &cpms)
	json.Unmarshal(lap_summaries_json, &lapSummaries)
	json.Unmarshal(end_summary_json, &endSummary)
	hasPower = has_power
	hasHeart = has_heart
	hasCadence = has_cadence
	curThr := cur_thr

	//count samples for calculations
	samples := len(powerSeries)

	//calulate power zones for this activity
	if hasPower {
		var sum int
		var average float64
		for i := user.SampleSize; i < samples; i++ {
			//reset total
			sum = 0
			//get thirty second rolling slice
			rollingPowerSlice := powerSeries[i-user.SampleSize : i]
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

	if hasHeart {
		for _, val := range heartSeries {
			if float64(val) < 0.81*float64(curThr) {
				zoneData.HR1++
			} else if float64(val) > 0.81*float64(curThr) && float64(val) <= 0.89*float64(curThr) {
				zoneData.HR2++
			} else if float64(val) > 0.89*float64(curThr) && float64(val) <= 0.93*float64(curThr) {
				zoneData.HR3++
			} else if float64(val) > 0.93*float64(curThr) && float64(val) <= 0.99*float64(curThr) {
				zoneData.HR4++
			} else if float64(val) > 0.99*float64(curThr) && float64(val) <= 1.02*float64(curThr) {
				zoneData.HR5a++
			} else if float64(val) > 1.02*float64(curThr) && float64(val) <= 1.06*float64(curThr) {
				zoneData.HR5b++
			} else if float64(val) > 1.06*float64(curThr) {
				zoneData.HR5c++
			}
		}
	}

	var body = []byte("Activity overview")

	//cp data - doesn't actually need to be reversed (corrected), but just wanted to for future flexibility
	cpRowsRev := make([]CpRow, 0)
	for i := len(cpRows) - 1; i >= 0; i-- {
		cpRowsRev = append(cpRowsRev, cpRows[i])
	}

	p = Page{
		Title:        title,
		ActivityMeta: meta,
		Body:         body,
		LapSummaries: lapSummaries,
		EndSummary:   endSummary,
		CPM:          cpms,
		CPData:       cpRowsRev,
		Data:         rows,
		HasPower:     hasPower,
		HasHeart:     hasHeart,
		HasCadence:   hasCadence,
		ZoneData:     zoneData,
		CurThr:       curThr,
		Theme:        user.Theme,
		Demo:         user.Demo,
		Message:      message,
	}
	return
}
