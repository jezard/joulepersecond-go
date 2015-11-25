package types

/* types shared across features */

import (
	"time"
)

type StandardRide struct {
	Id    int
	Label string
}

//user settings
type UserSettings struct {
	EncId         string
	Id            string //user me%40mydomain.co.uk
	Email         string //user me@mydomain.co.uk
	Paid_account  bool   //user has a subscription
	Atl_constant  int    //ATL constant - default 7 Days
	Ctl_constant  int    //CTL constant - default 42 Days
	Theme         string
	Demo          bool           //is this a demo?
	TimeOffset    int            //view dashboard history from a previous day - useful for testing if nothing else
	SampleSize    int            //might use this to adjust user selected sample size
	Ftp           int            //user's Functional Threshold Power
	Thr           int            //User's functional threshold Heartrate
	Ncp_rolloff   int            //User set Notable Critical Power performance rolloff constant
	Stopgap       time.Duration  //number of seconds to trigger auto removal from activity (default 15)
	Autofill      string         //whether to replace missing values with last recorded sample data (default), set to zero or remove. Options 'autofill', 'setzero', 'remove'
	Rhr           int            //user's resting heart rate
	Vo2           float32        //user's vo2 Max
	Gender        string         //user's gender
	Weight        int            //user's weight
	Age           int            //user's age
	StandardRides []StandardRide //user's standard rides
}

type CPMs struct {
	FiveSecondCP, TwentySecondCP, SixtySecondCP, FiveMinuteCP, TwentyMinuteCP, SixtyMinuteCP                   int
	FiveSecondCPHR, TwentySecondCPHR, SixtySecondCPHR, FiveMinuteCPHR, TwentyMinuteCPHR, SixtyMinuteCPHR       int
	FiveSecondCPCAD, TwentySecondCPCAD, SixtySecondCPCAD, FiveMinuteCPCAD, TwentyMinuteCPCAD, SixtyMinuteCPCAD int
}

type Tvd struct {
	TimeLabel string
	TotalTss  int
	TotalDur  float64 //in hours
}

type Tvd_data_point struct {
	Date time.Time
	Tss  int
	Dur  time.Duration
}

type ZoneLabels struct {
	PowerZ1, PowerZ2, PowerZ3, PowerZ4, PowerZ5, PowerZ6, HeartZ1, HeartZ2, HeartZ3, HeartZ4, HeartZ5a, HeartZ5b, HeartZ5c int
}

type Metrics struct {
	Avpower, Avheart, Avcad, Np, Tss, Etss, Utss, WorkDone, EnergyUsedKc, EnergyUsedKj, IfHr int //Utss will be used to store a user's overidden tss  [probably best to store these overrides in a seperate table]
	If                                                                                       float64
	StartTime                                                                                time.Time
	Dur                                                                                      time.Duration
}
type Current_ff struct {
	Ctl, Atl, Tsb int
}

//store various types of information about an activity - 1 record per activity
type ActivityMeta struct {
	ActivityName, ActivityID                      string
	TssOverride, MotivationLevel, PerceivedEffort int
	IndoorRide, OutdoorRide, Race, Train          bool
}

//data type for Fitness Freshness graph
type Ff_data_point struct {
	Date             time.Time
	Tss              int
	Ctl              float64
	Atl              float64
	Tsb              float64
	NotableCp        float64
	HasValue         bool
	Day, Month, Year int
	Meta             ActivityMeta
}

/* Power Zones:
Zone 1 Less than 55% of FTPw
Zone 2 55% to 74% of FTPw
Zone 3 75% to 89% of FTPw
Zone 4 90% to 104% of FTPw
Zone 5 105% to 120% of FTPw
Zone 6 More than 120% of FTPw
*/
type Zones struct {
	Z1, Z2, Z3, Z4, Z5, Z6, HR1, HR2, HR3, HR4, HR5a, HR5b, HR5c int
	HasPower, HasHeart                                           bool
}
