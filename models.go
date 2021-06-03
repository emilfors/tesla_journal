package main

import (
	"database/sql"
	"time"

	"github.com/lib/pq"
	geojson "github.com/paulmach/go.geojson"
)

const (
	unknown  = -1
	business = 1
	private  = 2
)

type Config struct {
	Connection struct {
		Host     string
		Port     int
		User     string
		Password string
		DB       string
	}
	Service struct {
		Port     int
		CertFile string
		KeyFile  string
	}
}

type Day struct {
	Date          time.Time
	DateString    string
	DateAsTs      int64
	Drives        []Drive
	GroupedDrives []GroupedDrives
}

func (d Day) GetGroupedDrives(id int) *GroupedDrives {
	for _, gd := range d.GroupedDrives {
		if gd.Id == id {
			return &gd
		}
	}

	return nil
}

func (d Day) IsWeekend() bool {
	switch d.Date.Weekday() {
	case time.Saturday:
		fallthrough
	case time.Sunday:
		return true
	}

	return false
}

type Drive struct {
	Id                   int
	StartDate            time.Time
	EndDate              time.Time
	StartTime            string
	EndTime              string
	Duration             int
	DurationString       string
	StartAddress         string
	EndAddress           string
	StartOdometer        int
	EndOdometer          int
	Distance             float32
	DistanceString       string
	Classification       sql.NullInt32
	ClassificationClass  string
	ClassificationString string
	GroupId              sql.NullInt32
	Comment              sql.NullString
}

func (d Drive) GroupIdInt() int {
	if d.GroupId.Valid {
		return int(d.GroupId.Int32)
	} else {
		return -1
	}
}

type GetDriveResponse struct {
	Drive   Drive
	Comment string
	MapData geojson.FeatureCollection
}

type GroupedDrives struct {
	Id                   int
	CarId                int
	DriveIds             pq.Int64Array
	StartDate            time.Time
	EndDate              time.Time
	StartTime            string
	EndTime              string
	StartAddress         string
	EndAddress           string
	StartOdometer        int
	EndOdometer          int
	Distance             float32
	DistanceString       string
	Duration             int
	DurationString       string
	Classification       sql.NullInt32
	ClassificationClass  string
	ClassificationString string
	Comment              sql.NullString
}

type GetGroupedDrivesResponse struct {
	Drives  GroupedDrives
	MapData geojson.FeatureCollection
}

type Car struct {
	Id    int
	Model string
	Name  string
}

type Month struct {
	Number int
	Name   string
}

type Totals struct {
	TotalDuration         int
	TotalBusinessDuration int
	TotalPrivateDuration  int
	TotalDistance         float32
	TotalBusinessDistance float32
	TotalPrivateDistance  float32
	UnclassifiedDuration  int
	UnclassifiedDistance  float32
}

type MainData struct {
	Year                        int
	Month                       int
	CarId                       int
	DropdownCars                []Car
	DropdownYears               []int
	DropdownMonths              []Month
	Days                        []Day
	TotalDurationString         string
	TotalBusinessDurationString string
	TotalPrivateDurationString  string
	TotalDistanceString         string
	TotalBusinessDistanceString string
	TotalPrivateDistanceString  string
	UnclassifiedDrivesRemaining bool
	UnclassifiedDurationString  string
	UnclassifiedDistanceString  string
}

func getClassificationId(classification string) int {
	switch classification {
	case "business":
		return business
	case "private":
		return private
	default:
		return unknown
	}
}

type Position struct {
	Longitude float64
	Latitude  float64
}
