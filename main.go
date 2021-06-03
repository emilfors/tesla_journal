package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/paulmach/go.geojson"
	"gopkg.in/gcfg.v1"
)

var mainTemplate *template.Template = template.Must(template.ParseFiles("main.html"))
var detailsTemplate *template.Template = template.Must(template.ParseFiles("details.html"))

func main() {
	var config Config

	// sane default config values:
	config.Connection.Host = "localhost"
	config.Connection.Port = 5432
	config.Connection.User = "teslamate"
	config.Connection.DB = "teslamate"
	config.Service.Port = 4001

	err := gcfg.ReadFileInto(&config, "tesla_journal.cfg")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to read configuration: %v\n", err)
		os.Exit(1)
	}

	err = connectDB(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	// start serving requests:
	port := strconv.Itoa(config.Service.Port)

	r := mux.NewRouter()

	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))))
	r.HandleFunc("/", serveGet).Methods(http.MethodGet)
	r.HandleFunc("/", servePost).Methods(http.MethodPost)
	r.HandleFunc("/details/{id}", serveDriveDetails).Methods(http.MethodGet)
	r.HandleFunc("/drive/{id}", getDriveDetails).Methods(http.MethodGet)
	r.HandleFunc("/drive/group/{id}", getGroupDriveDetails).Methods(http.MethodGet)
	r.HandleFunc("/groupdetails/{id}", serveGroupedDriveDetails).Methods(http.MethodGet)
	r.HandleFunc("/action", postAction).Methods(http.MethodPost)

	secure := config.Service.CertFile != "" && config.Service.KeyFile != ""
	if secure {
		fmt.Println("Listening on secure port " + port)
		err = http.ListenAndServeTLS(":"+port, config.Service.CertFile, config.Service.KeyFile, r)
		if err != nil {
			fmt.Println("Secure mode failed: " + err.Error())
			secure = false
		}
	}

	if !secure {
		fmt.Println("Listening on non-secure port " + port)
		err = http.ListenAndServe(":"+port, r)
	}

	log.Fatal(err)
}

func getIntParamPost(r *http.Request, param string, into *int) error {
	val, err := strconv.Atoi(r.Form.Get(param))
	if err != nil {
		return err
	}

	*into = val
	return nil
}

func serveGet(w http.ResponseWriter, r *http.Request) {
	year := int(time.Now().Year())
	month := int(time.Now().Month())
	car := 1

	data := generateMain(year, month, car)

	err := mainTemplate.Execute(w, data)
	if err != nil {
		log.Fatal("Error while executing template: " + err.Error())
	}
}

func servePost(w http.ResponseWriter, r *http.Request) {
	year := int(time.Now().Year())
	month := int(time.Now().Month())
	car := 1

	err := r.ParseForm()
	if err != nil {
		panic(err)
	}

	getIntParamPost(r, "year", &year)
	getIntParamPost(r, "month", &month)
	getIntParamPost(r, "car", &car)

	data := generateMain(year, month, car)

	err = mainTemplate.Execute(w, data)
	if err != nil {
		log.Fatal("Error while executing template: " + err.Error())
	}
}

func getDriveDetails(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	var ids []string
	ids = append(ids, vars["id"])

	positions, err := getPositions(ids)
	if err != nil {
		log.Println("Error while getting positions")
	}

	var coordinates [][]float64
	for _, pos := range positions {
		var c []float64
		c = append(c, pos.Longitude)
		c = append(c, pos.Latitude)
		coordinates = append(coordinates, c)
	}

	featureCollection := geojson.NewFeatureCollection()
	feature := geojson.NewLineStringFeature(coordinates)
	featureCollection.AddFeature(feature)

	var response GetDriveResponse
	response.MapData = *featureCollection
	response.Drive, response.Comment, err = getDriveById(vars["id"])
	if err != nil {
		log.Println("Error getting drive details: " + err.Error())
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func getGroupDriveDetails(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	var ids []string
	ids = append(ids, vars["id"])
	driveIds, err := getDriveIdsForGroups(ids)
	if err != nil {
		log.Println("Error while getting drive ids for grouped drive")
	}

	positions, err := getPositions(driveIds)
	if err != nil {
		log.Println("Error while getting positions")
	}

	var coordinates [][]float64
	for _, pos := range positions {
		var c []float64
		c = append(c, pos.Longitude)
		c = append(c, pos.Latitude)
		coordinates = append(coordinates, c)
	}

	featureCollection := geojson.NewFeatureCollection()
	feature := geojson.NewLineStringFeature(coordinates)
	featureCollection.AddFeature(feature)

	var response GetGroupedDrivesResponse
	response.MapData = *featureCollection
	response.Drives, err = getGroupedDrivesById(vars["id"])
	if err != nil {
		log.Println("Error getting drive details: " + err.Error())
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func serveDriveDetails(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	var data struct {
		Id    string
		Group bool
	}
	data.Id = vars["id"]
	data.Group = false

	err := detailsTemplate.Execute(w, data)
	if err != nil {
		log.Fatal("Error while executing template: " + err.Error())
	}
}

func serveGroupedDriveDetails(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	var data struct {
		Id    string
		Group bool
	}
	data.Id = vars["id"]
	data.Group = true

	err := detailsTemplate.Execute(w, data)
	if err != nil {
		log.Fatal("Error while executing template: " + err.Error())
	}
}

func postAction(w http.ResponseWriter, r *http.Request) {
	year := int(time.Now().Year())
	month := int(time.Now().Month())
	car := 1

	err := r.ParseForm()
	if err != nil {
		panic(err)
	}

	getIntParamPost(r, "year", &year)
	getIntParamPost(r, "month", &month)
	getIntParamPost(r, "car", &car)

	var from, to *time.Time

	action := r.Form.Get("action")
	if action == "classify" {
		from, to, err = changeClassification(getClassificationId(r.Form.Get("classification")), r.Form["drive"], r.Form["groupeddrive"])
		if err != nil {
			log.Println("Error changing drive classification")
		}
	} else if action == "group" {
		from, to, err = groupDrives(car, r.Form["drive"])
		if err != nil {
			log.Println("Error grouping drives")
		}
	} else if action == "ungroup" {
		from, to, err = ungroupDrives(car, r.Form["groupeddrive"])
		if err != nil {
			log.Println("Error ungrouping drives")
		}
	}

	var affectedDays []Day
	if from != nil && to != nil {
		affectedDays, err = getDays(*from, *to, car)
		if err != nil {
			log.Println("Error retrieving affected days")
		}
	} else {
		log.Println("The action did not return a useful date range")
	}

	totals, err := getTotals(year, month, car)
	if err != nil {
		log.Println("Error retrieving totals")
	}

	var response PostResponse
	response.Totals = totals
	response.AffectedDays = affectedDays

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

type PostResponse struct {
	Totals       Totals
	AffectedDays []Day
}
