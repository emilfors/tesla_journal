package main

import (
    "time"
)

func minutesToHoursAndMinutes(minutes int) (int, int) {
    h := 0
    m := minutes
    for m >= 60 {
        h += 1
        m -= 60
    }

    return h, m
}

func convertTime(t time.Time) time.Time {
    loc, err := time.LoadLocation("Europe/Stockholm")
    if err == nil {
        t = t.In(loc)
    }

    return t
}

func stripTime(from time.Time) time.Time {
    return time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
}

