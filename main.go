package main

import (
    "fmt"
    "log"
    "os"
    "time"
    "strings"
    "strconv"
    "errors"
    "net/http"
    "html/template"
    "github.com/gorilla/mux"
    _ "github.com/lib/pq"
    "github.com/goodsign/monday"
    "database/sql"
    "gopkg.in/gcfg.v1"
)

const (
    unknown = -1
    business = 1
    private = 2
)

type Config struct {
    Connection struct {
        Host                            string
        Port                            int
        User                            string
        Password                        string
        DB                              string
    }
}

type Day struct {
    Date                                time.Time
    DateString                          string
    DateAsTs                            int64
    Drives                              []Drive
}

type Drive struct {
    Id                                  int
    StartDate                           time.Time
    EndDate                             time.Time
    StartTime                           string
    EndTime                             string
    Duration                            int
    DurationString                      string
    StartAddress                        string
    EndAddress                          string
    StartOdometer                       int
    EndOdometer                         int
    Distance                            float32
    DistanceString                      string
    Classification                      sql.NullInt32
}

func (d Drive) ClassificationInt() int {
    if !d.Classification.Valid {
        return -1
    }

    return int(d.Classification.Int32)
}

func (d Drive) ClassificationClass() string {
    if !d.Classification.Valid {
        return "unknown"
    }

    switch d.Classification.Int32 {
        case business: return "business"
        case private: return "private"
        default: return "unknown"
    }
}

func (d Drive) ClassificationString() string {
    if !d.Classification.Valid {
        return ""
    }

    switch d.Classification.Int32 {
        case business: return "Tjänsteresa"
        case private: return "Privat resa"
        default: return ""
    }
}

type Car struct {
    Id                                  int
    Model                               string
    Name                                string
}

type Month struct {
    Number                              int
    Name                                string
}

type MainData struct {
    Year                                int
    Month                               int
    CarId                               int
    DropdownCars                        []Car
    DropdownYears                       []int
    DropdownMonths                      []Month
    Days                                []Day
    TotalDurationString                 string
    TotalBusinessDurationString         string
    TotalPrivateDurationString          string
    TotalDistanceString                 string
    TotalBusinessDistanceString         string
    TotalPrivateDistanceString          string
}

var db *sql.DB
var config Config

var mainTemplate *template.Template = template.Must(template.ParseFiles("main.html"))

func main() {
    var err error

    err = gcfg.ReadFileInto(&config, "tesla_journal.cfg")
    if err != nil {
        fmt.Fprintf(os.Stderr, "Unable to read configuration: %v\n", err)
        os.Exit(1)
    }

    conn := config.Connection
    psqlInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", conn.Host, conn.Port, conn.User, conn.Password, conn.DB)
    db, err = sql.Open("postgres", psqlInfo)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
        os.Exit(1)
    }
    defer db.Close()

    err = db.Ping()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Unable to open database: %v\n", err)
        os.Exit(1)
    }

    fmt.Println("Connected to database " + conn.DB)

    err = createTables()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Unable to create necessary tables: %v\n", err)
        os.Exit(1)
    }

    // start serving requests:
    port := "80"

    r := mux.NewRouter()
    r.HandleFunc("/", serveGet).Methods(http.MethodGet)
    r.HandleFunc("/", servePost).Methods(http.MethodPost)

    fmt.Println("Now listening on port " + port)

    log.Fatal(http.ListenAndServe(":" + port, r))
}

func getIntParam(r *http.Request, param string, into *int) error {
    keys, ok := r.URL.Query()[param]
    if !ok || len(keys[0]) < 1 {
        return errors.New("No such parameter: " + param)
    } else {
        val, err := strconv.Atoi(string(keys[0]))
        if err != nil {
            return err
        }

        *into = val
        return nil
    }
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
    if (err != nil) {
        log.Fatal("Error while executing template: " + err.Error())
    }
}

func getClassificationId(classification string) int {
    switch classification {
        case "business": return business
        case "private": return private
        default: return unknown
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

    action := r.Form.Get("action")
    if action == "classify" {
        err = changeClassification(getClassificationId(r.Form.Get("classification")), r.Form["drive"])
        if err != nil {
            log.Println("Error changing drive classification")
        }
    }

    data := generateMain(year, month, car)

    err = mainTemplate.Execute(w, data)
    if (err != nil) {
        log.Fatal("Error while executing template: " + err.Error())
    }
}

func changeClassification(classification int, drives []string) error {
    if len(drives) == 0 {
        return errors.New("Attempt to classify drives failed; no drive ids specified")
    }

    statement := "INSERT INTO public.classifications (drive_id, classification) VALUES "
    for _, driveId := range drives {
        statement += fmt.Sprintf("(%s, %d),", driveId, classification)
    }
    statement = strings.TrimRight(statement, ",")
    statement += " ON CONFLICT(drive_id) DO UPDATE SET classification = excluded.classification;"

    _, err := db.Exec(statement)
    if err != nil {
        return err
    }

    return nil
}

func generateMain(year, month, carId int) MainData {
    var data MainData

    data.Year = year
    data.Month = month
    data.CarId = carId

    cars, err := getCars()
    if err != nil {
        log.Println("Error retrieving cars from database")
    }
    data.DropdownCars = cars

    from := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
    to := from.AddDate(0, 1, 0).Add(time.Millisecond * -1)

    drives, err := getDrives(carId, from, to)
    if err != nil {
        log.Println("Error retrieving drives from database")
    }

    var days []Day
    var day *Day = nil
    current := -1
    for _, drive := range drives {
        d := drive.StartDate.Day()
        if d != current {
            if day != nil {
                days = append(days, *day)
            }

            day = new(Day)
            day.Date = drive.StartDate

            t := convertTime(day.Date)
            day.DateString = strings.ToUpper(monday.Format(t, "Monday 2 January", monday.LocaleSvSE))
            day.DateAsTs = day.Date.Unix()

            current = d
        }

        day.Drives = append(day.Drives, drive)
    }

    if day != nil {
        days = append(days, *day)
    }
    data.Days = days

    data.DropdownYears = make([]int, 0)
    data.DropdownYears = append(data.DropdownYears, 2020)

    data.DropdownMonths = make([]Month, 0)
    data.DropdownMonths = append(data.DropdownMonths, Month{1, "Januari"})
    data.DropdownMonths = append(data.DropdownMonths, Month{2, "Februari"})
    data.DropdownMonths = append(data.DropdownMonths, Month{3, "Mars"})
    data.DropdownMonths = append(data.DropdownMonths, Month{4, "April"})
    data.DropdownMonths = append(data.DropdownMonths, Month{5, "Maj"})
    data.DropdownMonths = append(data.DropdownMonths, Month{6, "Juni"})
    data.DropdownMonths = append(data.DropdownMonths, Month{7, "Juli"})
    data.DropdownMonths = append(data.DropdownMonths, Month{8, "Augusti"})
    data.DropdownMonths = append(data.DropdownMonths, Month{9, "September"})
    data.DropdownMonths = append(data.DropdownMonths, Month{10, "Oktober"})
    data.DropdownMonths = append(data.DropdownMonths, Month{11, "November"})
    data.DropdownMonths = append(data.DropdownMonths, Month{12, "December"})


    var totalDuration, totalBusinessDuration, totalPrivateDuration int
    var totalDistance, totalBusinessDistance, totalPrivateDistance float32

    for _, day := range data.Days {
        for _, drive := range day.Drives {
            totalDuration += drive.Duration
            totalDistance += drive.Distance

            if drive.Classification.Valid {
                switch drive.Classification.Int32 {
                case business:
                    totalBusinessDuration += drive.Duration
                    totalBusinessDistance += drive.Distance

                case private:
                    totalPrivateDuration += drive.Duration
                    totalPrivateDistance += drive.Distance
                }
            }
        }
    }

    h, m := minutesToHoursAndMinutes(totalDuration)
    data.TotalDurationString = fmt.Sprintf("%d:%02d", h, m)
    data.TotalDistanceString = fmt.Sprintf("%.1f", totalDistance)

    h, m = minutesToHoursAndMinutes(totalBusinessDuration)
    data.TotalBusinessDurationString = fmt.Sprintf("%d:%02d", h, m)
    data.TotalBusinessDistanceString = fmt.Sprintf("%.1f", totalBusinessDistance)

    h, m = minutesToHoursAndMinutes(totalPrivateDuration)
    data.TotalPrivateDurationString = fmt.Sprintf("%d:%02d", h, m)
    data.TotalPrivateDistanceString = fmt.Sprintf("%.1f", totalPrivateDistance)

    return data
}

func minutesToHoursAndMinutes(minutes int) (int, int) {
    h := 0
    m := minutes
    for m >= 60 {
        h += 1
        m -= 60
    }

    return h, m
}

func getDrives(carId int, from, to time.Time) ([]Drive, error) {
    statement := fmt.Sprintf(`WITH data AS (
        SELECT
        round(extract(epoch FROM start_date)) * 1000 AS start_date_ts,
        round(extract(epoch FROM end_date)) * 1000 AS end_date_ts,
        car.id as car_id,
        start_km,
        end_km,
        CASE WHEN start_geofence.id IS NULL THEN CONCAT('new?lat=', start_position.latitude, '&lng=', start_position.longitude)
        WHEN start_geofence.id IS NOT NULL THEN CONCAT(start_geofence.id, '/edit')
        END as start_path,
        CASE WHEN end_geofence.id IS NULL THEN CONCAT('new?lat=', end_position.latitude, '&lng=', end_position.longitude)
        WHEN end_geofence.id IS NOT NULL THEN CONCAT(end_geofence.id, '/edit')
        END as end_path,
        drives.id as drive_id,
        classification.classification,
        start_date,
        end_date,
        duration_min,
        COALESCE(start_geofence.name, CONCAT_WS(', ', COALESCE(start_address.name, nullif(CONCAT_WS(' ', start_address.road, start_address.house_number), '')), start_address.city)) AS start_address,
        COALESCE(end_geofence.name, CONCAT_WS(', ', COALESCE(end_address.name, nullif(CONCAT_WS(' ', end_address.road, end_address.house_number), '')), end_address.city)) AS end_address,
        distance
        FROM drives
        LEFT JOIN addresses start_address ON start_address_id = start_address.id
        LEFT JOIN addresses end_address ON end_address_id = end_address.id
        LEFT JOIN positions start_position ON start_position_id = start_position.id
        LEFT JOIN positions end_position ON end_position_id = end_position.id
        LEFT JOIN geofences start_geofence ON start_geofence_id = start_geofence.id
        LEFT JOIN geofences end_geofence ON end_geofence_id = end_geofence.id
        LEFT JOIN cars car ON car.id = drives.car_id
        LEFT JOIN classifications classification ON classification.drive_id = drives.id
        WHERE drives.car_id = %d AND start_date >= '%s'::date AND start_date <= '%s'::date
        ORDER BY start_date DESC
    )
    SELECT
    drive_id,
    start_date,
    end_date,
    duration_min,
    start_address,
    end_address,
    round(start_km::numeric) AS start_odo,
    round(end_km::numeric) AS end_odo,
    distance,
    classification
    FROM data;`, carId, from.Format("2006-01-02 15:04:05.000"), to.Format("2006-01-02 15:04:05.000"))

    var drives []Drive

    rows, _ := db.Query(statement)
    defer rows.Close()

    for rows.Next() {
        var drive Drive

        err := rows.Scan(&drive.Id, &drive.StartDate, &drive.EndDate, &drive.Duration, &drive.StartAddress, &drive.EndAddress, &drive.StartOdometer, &drive.EndOdometer, &drive.Distance, &drive.Classification)
        if err != nil {
            return nil, err
        }

        drive.StartTime = convertTime(drive.StartDate).Format("15:04")
        drive.EndTime = convertTime(drive.EndDate).Format("15:04")

        h, m := minutesToHoursAndMinutes(drive.Duration)
        drive.DurationString = fmt.Sprintf("%d:%02d", h, m)
        drive.DistanceString = fmt.Sprintf("%.2f", drive.Distance)

        drives = append(drives, drive)
    }

    return drives, rows.Err()
}

func getCars() ([]Car, error) {
    var cars []Car

    statement := "SELECT id, model, name FROM cars ORDER BY id ASC;"

    rows, _ := db.Query(statement)
    defer rows.Close()

    for rows.Next() {
        var car Car

        err := rows.Scan(&car.Id, &car.Model, &car.Name)
        if err != nil {
            return nil, err
        }

        cars = append(cars, car)
    }

    return cars, rows.Err()
}

func convertTime(t time.Time) time.Time {
    loc, err := time.LoadLocation("Europe/Stockholm")
    if err == nil {
        t = t.In(loc)
    }

    return t
}

func createTables() error {
    statement := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS public.classifications
    (
        drive_id integer NOT NULL,
        classification integer,
        PRIMARY KEY (drive_id)
    );
    ALTER TABLE public.classifications
    OWNER to %s;`, config.Connection.User)

    _, err := db.Exec(statement)
    if err != nil {
        return err
    }

    fmt.Println("Classifications table exists.")

    return nil
}

