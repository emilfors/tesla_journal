package main

import (
    "fmt"
    "log"
    "os"
    "time"
    "strconv"
    "errors"
    "net/http"
    "html/template"
    "github.com/gorilla/mux"
    "gopkg.in/gcfg.v1"
    "encoding/json"
)

var mainTemplate *template.Template = template.Must(template.ParseFiles("main.html"))

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
    defer db.Close()

    // start serving requests:
    port := strconv.Itoa(config.Service.Port)

    r := mux.NewRouter()

    r.PathPrefix("/js/").Handler(http.StripPrefix("/js/", http.FileServer(http.Dir("./js/"))))
    r.HandleFunc("/", serveGet).Methods(http.MethodGet)
    r.HandleFunc("/", servePost).Methods(http.MethodPost)
    r.HandleFunc("/action", postAction).Methods(http.MethodPost)
    r.HandleFunc("/day/{year}/{month}/{day}/{car}", getDay).Methods(http.MethodGet)

    secure := config.Service.CertFile != "" && config.Service.KeyFile != ""
    if secure {
        fmt.Println("Listening on secure port " + port)
        err = http.ListenAndServeTLS(":" + port, config.Service.CertFile, config.Service.KeyFile, r)
        if err != nil {
            fmt.Println("Secure mode failed: " + err.Error())
            secure = false
        }
    }

    if !secure {
        fmt.Println("Listening on non-secure port " + port)
        err = http.ListenAndServe(":" + port, r)
    }

    log.Fatal(err)
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
    if (err != nil) {
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

    action := r.Form.Get("action")
    if action == "classify" {
        err = changeClassification(getClassificationId(r.Form.Get("classification")), r.Form["drive"], r.Form["groupeddrive"])
        if err != nil {
            log.Println("Error changing drive classification")
        }
    } else if action == "group" {
        err = groupDrives(car, r.Form["drive"])
        if err != nil {
            log.Println("Error grouping drives")
        }
    } else if action == "ungroup" {
        err = ungroupDrives(car, r.Form["groupeddrive"])
        if err != nil {
            log.Println("Error ungrouping drives")
        }
    }

    affectedDays, err := getAffectedDaysFromDB(year, month, car, r.Form["drive"], r.Form["groupeddrive"])
    if err != nil {
        log.Println("Error retrieving affected days")
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
    Totals          Totals
    AffectedDays    []Day
}

func getDay(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)

    y, err := strconv.Atoi(vars["year"])
    if err != nil {
        w.WriteHeader(http.StatusBadRequest)
        return
    }

    m, err := strconv.Atoi(vars["month"])
    if err != nil {
        w.WriteHeader(http.StatusBadRequest)
        return
    }

    d, err := strconv.Atoi(vars["day"])
    if err != nil {
        w.WriteHeader(http.StatusBadRequest)
        return
    }

    c, err := strconv.Atoi(vars["car"])
    if err != nil {
        w.WriteHeader(http.StatusBadRequest)
        return
    }

    day, err := getDayFromDB(y, m, d, c)
    if err != nil {
        w.WriteHeader(http.StatusNotFound)
        return
    }

    w.WriteHeader(http.StatusOK)
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(day)
}

