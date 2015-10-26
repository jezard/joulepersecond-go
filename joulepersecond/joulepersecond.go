package main

import (
	"github.com/jezard/joulepersecond-go/activity"
	"github.com/jezard/joulepersecond-go/analysis"
	"github.com/jezard/joulepersecond-go/dashboard"
	"net/http"
)

func main() {

	//test dashboard
	http.HandleFunc("/dashboard/", dashboard.DashboardHandler)

	//activity routes
	http.HandleFunc("/activity", activity.ActivityHandler)
	http.HandleFunc("/view/activity/", activity.ActivityHandler)
	http.HandleFunc("/process/activity/", activity.ActivityHandler)
	http.HandleFunc("/process/file/", activity.ActivityHandler)
	http.HandleFunc("/delete/activity/", activity.ActivityHandler)

	//analysis routes
	http.HandleFunc("/analysis?", analysis.AnalysisHandler)
	http.HandleFunc("/analysis/", analysis.AnalysisHandler)

	//static file handler.
	http.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))))

	//Listen on port 8080
	http.ListenAndServe(":8080", nil)
}
