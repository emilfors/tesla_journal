package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/goodsign/monday"
	"github.com/lib/pq"
)

var database *sql.DB
var config Config

func connectDB(conf Config) error {
	var err error
	config = conf
	conn := config.Connection

	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", conn.Host, conn.Port, conn.User, conn.Password, conn.DB)
	database, err = sql.Open("postgres", psqlInfo)
	if err != nil {
		return err
	}

	// sql.Open doesn't actually open the database; ping it for that to happen:
	err = database.Ping()
	if err != nil {
		return err
	}

	fmt.Println("Connected to database " + conn.DB)

	err = createTables()
	if err != nil {
		return err
	}

	return nil
}

func db() *sql.DB {
	err := database.Ping()
	if err != nil {
		err = connectDB(config)
		if err != nil {
			panic(err)
		}
	}

	return database
}

func changeClassification(classification int, drives []string, groupedDrives []string) (*time.Time, *time.Time, error) {
	if (len(drives) + len(groupedDrives)) == 0 {
		return nil, nil, errors.New("Attempt to classify drives failed; no drive ids or grouped drive ids specified")
	}

	groupedDriveIds, err := getDriveIdsForGroups(groupedDrives)
	if err != nil {
		return nil, nil, err
	}

	ids := append(drives, groupedDriveIds...)

	statement := "INSERT INTO public.tj_classifications (drive_id, classification) VALUES "
	for _, driveId := range ids {
		statement += fmt.Sprintf("(%s, %d),", driveId, classification)
	}
	statement = strings.TrimRight(statement, ",")
	statement += " ON CONFLICT(drive_id) DO UPDATE SET classification = excluded.classification;"

	_, err = db().Exec(statement)
	if err != nil {
		return nil, nil, err
	}

	if len(groupedDrives) != 0 {
		statement = fmt.Sprintf(`
        UPDATE public.tj_grouped_drives
        SET classification=%d
        WHERE id=ANY('{`, classification)
		for _, gid := range groupedDrives {
			statement += gid + ","
		}
		statement = strings.TrimRight(statement, ",")
		statement += `}');`

		_, err = db().Exec(statement)
		if err != nil {
			return nil, nil, err
		}
	}

	return getAffectedDates(drives, groupedDrives)
}

func getDriveIdsForGroups(groupedDrives []string) ([]string, error) {
	var groupedDriveIds []string

	statement := `
    SELECT drive_ids::text[]
    FROM tj_grouped_drives
    WHERE id=ANY('{`
	for _, gid := range groupedDrives {
		statement += gid + ","
	}
	statement = strings.TrimRight(statement, ",")
	statement += `}');`

	rows, _ := db().Query(statement)
	defer rows.Close()

	for rows.Next() {
		var ids pq.StringArray

		err := rows.Scan(&ids)
		if err != nil {
			return nil, err
		}

		groupedDriveIds = append(groupedDriveIds, ids...)
	}

	return groupedDriveIds, rows.Err()
}

func ungroupDrives(car int, groupedDrives []string) (*time.Time, *time.Time, error) {
	if len(groupedDrives) == 0 {
		return nil, nil, errors.New("Attempt to ungroup drives failed; no group drive ids specified")
	}

	from, to, _ := getAffectedDates([]string{}, groupedDrives)

	statement := `
    DELETE FROM tj_grouped_drives
    WHERE id=ANY('{`
	for _, gid := range groupedDrives {
		statement += gid + ","
	}
	statement = strings.TrimRight(statement, ",")
	statement += "}');"

	_, err := db().Exec(statement)
	if err != nil {
		return nil, nil, err
	}

	return from, to, err
}

func groupDrives(car int, drives []string) (*time.Time, *time.Time, error) {
	if len(drives) == 0 {
		return nil, nil, errors.New("Attempt to group drives failed; no drive ids specified")
	}

	statement := fmt.Sprintf(`
    SELECT
    min(start_date) AS start_date,
    max(end_date) AS end_date,
    sum(duration_min) AS duration,
    sum(distance) AS distance,
    max(start_address) start_address,
    max(end_address) end_address,
    CASE count(distinct classification) WHEN 1 THEN
        CASE count(classification)=count(*) WHEN true THEN min(classification) ELSE NULL END ELSE NULL END classification
    FROM
    (
        SELECT
            start_date,
            end_date,
            duration_min,
            distance,
            first_value(COALESCE(start_geofence.name, CONCAT_WS(', ', COALESCE(start_address.name, nullif(CONCAT_WS(' ', start_address.road, start_address.house_number), '')), start_address.city))) over (order by start_date asc) start_address,
            first_value(COALESCE(end_geofence.name, CONCAT_WS(', ', COALESCE(end_address.name, nullif(CONCAT_WS(' ', end_address.road, end_address.house_number), '')), end_address.city))) over (order by end_date desc) end_address,
            classification
        FROM drives
            LEFT JOIN addresses start_address ON start_address_id = start_address.id
            LEFT JOIN addresses end_address ON end_address_id = end_address.id
            LEFT JOIN geofences start_geofence ON start_geofence_id = start_geofence.id
            LEFT JOIN geofences end_geofence ON end_geofence_id = end_geofence.id
            LEFT JOIN tj_classifications classification ON classification.drive_id = drives.id
        WHERE drives.car_id=%d AND drives.id=ANY('{`, car)
	for _, driveId := range drives {
		statement += driveId + ","
	}
	statement = strings.TrimRight(statement, ",")
	statement += `}')
    ) c;`

	var startDate, endDate time.Time
	var duration int
	var distance float32
	var startAddress, endAddress string
	var classification sql.NullInt32

	row := db().QueryRow(statement)
	err := row.Scan(&startDate, &endDate, &duration, &distance, &startAddress, &endAddress, &classification)
	if err != nil {
		return nil, nil, err
	}

	statement = fmt.Sprintf(`
    INSERT INTO public.tj_grouped_drives
    (car_id, drive_ids, start_date, end_date, start_address, end_address, distance, duration_min, classification)
    VALUES
    (%d, '{`, car)
	for _, driveId := range drives {
		statement += driveId + ","
	}
	statement = strings.TrimRight(statement, ",")
	statement += "}', "
	statement += "'" + startDate.Format("2006-01-02 15:04:05.000") + "'::timestamp, "
	statement += "'" + endDate.Format("2006-01-02 15:04:05.000") + "'::timestamp, "
	statement += "'" + startAddress + "', "
	statement += "'" + endAddress + "', "
	statement += fmt.Sprintf("%f, ", distance)
	statement += fmt.Sprintf("%d, ", duration)
	if classification.Valid {
		statement += fmt.Sprintf("%d", classification.Int32)
	} else {
		statement += "null"
	}
	statement += ");"

	_, err = db().Exec(statement)
	if err != nil {
		return nil, nil, err
	}

	return getAffectedDates(drives, []string{})
}

func getFirstAndLastYears() (int, int, error) {
	statement := `
    SELECT
        min(start_date) as min_date,
        max(start_date) as max_date
    FROM drives`

	var minDate, maxDate time.Time

	row := db().QueryRow(statement)
	err := row.Scan(&minDate, &maxDate)
	if err != nil {
		return 0, 0, err
	}

	return minDate.Year(), maxDate.Year(), nil
}

func generateMain(year, month, carId int) MainData {
	var data MainData

	data.Year = year
	data.Month = month
	data.CarId = carId

	cars, err := getCars()
	if err != nil {
		log.Println("Error retrieving cars from database: " + err.Error())
	}
	data.DropdownCars = cars

	from := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)

	drives, err := getDrives(carId, from, to)
	if err != nil {
		log.Println("Error retrieving drives from database: " + err.Error())
	}

	groupedDrives, err := getGroupedDrives(carId, from, to)
	if err != nil {
		log.Println("Error retrieving grouped drives from database: " + err.Error())
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
			day.Date = stripTime(drive.StartDate)

			if gd, exists := groupedDrives[day.Date]; exists {
				day.GroupedDrives = gd
			}

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
	firstYear, lastYear, err := getFirstAndLastYears()
	if err != nil {
		log.Println("Error retrieving year span")
		firstYear = 2020
		lastYear = 2020
	}

	for y := firstYear; y <= lastYear; y++ {
		data.DropdownYears = append(data.DropdownYears, y)
	}

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

	var totalDuration, totalBusinessDuration, totalPrivateDuration, unclassifiedDuration int
	var totalDistance, totalBusinessDistance, totalPrivateDistance, unclassifiedDistance float32

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
			} else {
				unclassifiedDuration += drive.Duration
				unclassifiedDistance += drive.Distance
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

	if unclassifiedDuration > 0 || unclassifiedDistance > 0 {
		data.UnclassifiedDrivesRemaining = true
		h, m = minutesToHoursAndMinutes(unclassifiedDuration)
		data.UnclassifiedDurationString = fmt.Sprintf("%d:%02d", h, m)
		data.UnclassifiedDistanceString = fmt.Sprintf("%.1f", unclassifiedDistance)
	}

	return data
}

func getDay(year, month, day, carId int) (Day, error) {
	var d Day

	from := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)

	drives, err := getDrives(carId, from, to)
	if err != nil {
		log.Println("Error retrieving drives from database: " + err.Error())
	}
	d.Drives = drives

	groupedDrives, err := getGroupedDrives(carId, from, to)
	if err != nil {
		log.Println("Error retrieving grouped drives from database: " + err.Error())
	}

	d.Date = stripTime(from)

	if gd, exists := groupedDrives[d.Date]; exists {
		d.GroupedDrives = gd
	}

	t := convertTime(d.Date)
	d.DateString = strings.ToUpper(monday.Format(t, "Monday 2 January", monday.LocaleSvSE))
	d.DateAsTs = d.Date.Unix()

	return d, nil
}

func getDays(from, to time.Time, carId int) ([]Day, error) {
	drives, err := getDrives(carId, from, to)
	if err != nil {
		log.Println("Error retrieving drives from database: " + err.Error())
	}

	groupedDrives, err := getGroupedDrives(carId, from, to)
	if err != nil {
		log.Println("Error retrieving grouped drives from database: " + err.Error())
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
			day.Date = stripTime(drive.StartDate)

			if gd, exists := groupedDrives[day.Date]; exists {
				day.GroupedDrives = gd
			}

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

	return days, nil
}

func getAffectedDates(driveIds []string, groupedDriveIds []string) (*time.Time, *time.Time, error) {
	var dates []time.Time

	if len(driveIds) > 0 {
		statement := `
        SELECT
            min(start_date) as min_date,
            max(end_date) as max_date
        FROM drives
        WHERE id = ANY('{`
		for _, id := range driveIds {
			statement += id + ","
		}
		statement = strings.TrimRight(statement, ",")
		statement += `}')`

		var minD, maxD time.Time

		row := db().QueryRow(statement)
		err := row.Scan(&minD, &maxD)
		if err == nil {
			dates = append(dates, minD)
			dates = append(dates, maxD)
		}
	}

	if len(groupedDriveIds) > 0 {
		statement := `
        SELECT
            min(start_date) as min_date,
            max(end_date) as max_date
        FROM tj_grouped_drives
        WHERE id = ANY('{`
		for _, id := range groupedDriveIds {
			statement += id + ","
		}
		statement = strings.TrimRight(statement, ",")
		statement += `}')`

		var minD, maxD time.Time

		row := db().QueryRow(statement)
		err := row.Scan(&minD, &maxD)
		if err == nil {
			dates = append(dates, minD)
			dates = append(dates, maxD)
		}
	}

	if len(dates) < 2 {
		return nil, nil, errors.New("Error retrieving first/last dates of range of drives")
	}

	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
	minDate := stripTime(dates[0])
	maxDate := stripTime(dates[len(dates)-1]).AddDate(0, 0, 1)

	return &minDate, &maxDate, nil
}

func getDrives(carId int, from, to time.Time) ([]Drive, error) {
	statement := fmt.Sprintf(`WITH data AS (
        SELECT
        round(extract(epoch FROM drives.start_date)) * 1000 AS start_date_ts,
        round(extract(epoch FROM drives.end_date)) * 1000 AS end_date_ts,
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
        grouped_drive.id AS grouped_drive_id,
        drives.start_date,
        drives.end_date,
        drives.duration_min,
        COALESCE(start_geofence.name, CONCAT_WS(', ', COALESCE(start_address.name, nullif(CONCAT_WS(' ', start_address.road, start_address.house_number), '')), start_address.city)) AS start_address,
        COALESCE(end_geofence.name, CONCAT_WS(', ', COALESCE(end_address.name, nullif(CONCAT_WS(' ', end_address.road, end_address.house_number), '')), end_address.city)) AS end_address,
        drives.distance
        FROM drives
        LEFT JOIN addresses start_address ON start_address_id = start_address.id
        LEFT JOIN addresses end_address ON end_address_id = end_address.id
        LEFT JOIN positions start_position ON start_position_id = start_position.id
        LEFT JOIN positions end_position ON end_position_id = end_position.id
        LEFT JOIN geofences start_geofence ON start_geofence_id = start_geofence.id
        LEFT JOIN geofences end_geofence ON end_geofence_id = end_geofence.id
        LEFT JOIN cars car ON car.id = drives.car_id
        LEFT JOIN tj_classifications classification ON classification.drive_id = drives.id
        LEFT JOIN tj_grouped_drives grouped_drive ON drives.car_id=grouped_drive.car_id AND drives.id = ANY(grouped_drive.drive_ids)
        WHERE drives.car_id = %d AND drives.start_date >= '%s'::date AND drives.start_date < '%s'::date AND drives.start_date IS NOT NULL AND drives.end_date IS NOT NULL
        ORDER BY drives.start_date DESC
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
    classification,
    grouped_drive_id
    FROM data;`, carId, from.Format("2006-01-02 15:04:05.000"), to.Format("2006-01-02 15:04:05.000"))

	var drives []Drive

	rows, _ := db().Query(statement)
	defer rows.Close()

	for rows.Next() {
		var drive Drive

		err := rows.Scan(&drive.Id, &drive.StartDate, &drive.EndDate, &drive.Duration, &drive.StartAddress, &drive.EndAddress, &drive.StartOdometer, &drive.EndOdometer, &drive.Distance, &drive.Classification, &drive.GroupId)
		if err != nil {
			return nil, err
		}

		drive.ClassificationClass = "unknown"
		drive.ClassificationString = ""
		if drive.Classification.Valid {
			switch drive.Classification.Int32 {
			case business:
				drive.ClassificationClass = "business"
				drive.ClassificationString = "Tjänsteresa"

			case private:
				drive.ClassificationClass = "private"
				drive.ClassificationString = "Privat resa"
			}
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

func getGroupedDrivesById(id string) (GroupedDrives, error) {
	statement := fmt.Sprintf(`
    SELECT gd.*, round(MIN(d.start_km)), round(MAX(d.end_km))
	FROM tj_grouped_drives AS gd
    LEFT JOIN drives d ON d.id = ANY(gd.drive_ids)
	WHERE gd.id = %s
	GROUP BY gd.id`, id)

	var gd GroupedDrives

	row := db().QueryRow(statement)
	err := row.Scan(&gd.Id, &gd.CarId, &gd.DriveIds, &gd.StartDate, &gd.EndDate, &gd.StartAddress, &gd.EndAddress, &gd.Distance, &gd.Duration, &gd.Classification, &gd.Comment, &gd.StartOdometer, &gd.EndOdometer)
	if err != nil {
		return gd, err
	}

	gd.ClassificationClass = "unknown"
	gd.ClassificationString = ""
	if gd.Classification.Valid {
		switch gd.Classification.Int32 {
		case business:
			gd.ClassificationClass = "business"
			gd.ClassificationString = "Tjänsteresa"

		case private:
			gd.ClassificationClass = "private"
			gd.ClassificationString = "Privat resa"
		}
	}

	gd.StartTime = convertTime(gd.StartDate).Format("15:04")
	gd.EndTime = convertTime(gd.EndDate).Format("15:04")

	h, m := minutesToHoursAndMinutes(gd.Duration)
	gd.DurationString = fmt.Sprintf("%d:%02d", h, m)
	gd.DistanceString = fmt.Sprintf("%.2f", gd.Distance)

	return gd, nil
}

func getGroupedDrives(carId int, from, to time.Time) (map[time.Time][]GroupedDrives, error) {
	groupedDrives := make(map[time.Time][]GroupedDrives)

	statement := fmt.Sprintf(`
    SELECT gd.*, round(MIN(d.start_km)), round(MAX(d.end_km))
	FROM tj_grouped_drives AS gd
    LEFT JOIN drives d ON d.id = ANY(gd.drive_ids)
    WHERE gd.car_id = %d AND gd.start_date >= '%s'::date AND gd.start_date < '%s'::date
    GROUP BY gd.id`, carId, from.Format("2006-01-02 15:04:05.000"), to.Format("2006-01-02 15:04:05.000"))

	rows, err := db().Query(statement)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var gd GroupedDrives

		err := rows.Scan(&gd.Id, &gd.CarId, &gd.DriveIds, &gd.StartDate, &gd.EndDate, &gd.StartAddress, &gd.EndAddress, &gd.Distance, &gd.Duration, &gd.Classification, &gd.Comment, &gd.StartOdometer, &gd.EndOdometer)
		if err != nil {
			return nil, err
		}

		gd.ClassificationClass = "unknown"
		gd.ClassificationString = ""
		if gd.Classification.Valid {
			switch gd.Classification.Int32 {
			case business:
				gd.ClassificationClass = "business"
				gd.ClassificationString = "Tjänsteresa"

			case private:
				gd.ClassificationClass = "private"
				gd.ClassificationString = "Privat resa"
			}
		}

		gd.StartTime = convertTime(gd.StartDate).Format("15:04")
		gd.EndTime = convertTime(gd.EndDate).Format("15:04")

		h, m := minutesToHoursAndMinutes(gd.Duration)
		gd.DurationString = fmt.Sprintf("%d:%02d", h, m)
		gd.DistanceString = fmt.Sprintf("%.2f", gd.Distance)

		key := stripTime(gd.StartDate)
		_, ok := groupedDrives[key]
		if !ok {
			groupedDrives[key] = make([]GroupedDrives, 0)
		}

		groupedDrives[key] = append(groupedDrives[key], gd)
	}

	return groupedDrives, rows.Err()
}

func getDriveById(driveId string) (Drive, string, error) {
	statement := fmt.Sprintf(`WITH data AS (
        SELECT
        round(extract(epoch FROM drives.start_date)) * 1000 AS start_date_ts,
        round(extract(epoch FROM drives.end_date)) * 1000 AS end_date_ts,
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
        grouped_drive.id AS grouped_drive_id,
        drives.start_date,
        drives.end_date,
        drives.duration_min,
        COALESCE(start_geofence.name, CONCAT_WS(', ', COALESCE(start_address.name, nullif(CONCAT_WS(' ', start_address.road, start_address.house_number), '')), start_address.city)) AS start_address,
        COALESCE(end_geofence.name, CONCAT_WS(', ', COALESCE(end_address.name, nullif(CONCAT_WS(' ', end_address.road, end_address.house_number), '')), end_address.city)) AS end_address,
        drives.distance
        FROM drives
        LEFT JOIN addresses start_address ON start_address_id = start_address.id
        LEFT JOIN addresses end_address ON end_address_id = end_address.id
        LEFT JOIN positions start_position ON start_position_id = start_position.id
        LEFT JOIN positions end_position ON end_position_id = end_position.id
        LEFT JOIN geofences start_geofence ON start_geofence_id = start_geofence.id
        LEFT JOIN geofences end_geofence ON end_geofence_id = end_geofence.id
        LEFT JOIN cars car ON car.id = drives.car_id
        LEFT JOIN tj_classifications classification ON classification.drive_id = drives.id
        LEFT JOIN tj_grouped_drives grouped_drive ON drives.car_id=grouped_drive.car_id AND drives.id = ANY(grouped_drive.drive_ids)
        WHERE drives.id = %s
        ORDER BY drives.start_date DESC
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
    classification,
    grouped_drive_id
    FROM data;`, driveId)

	var drive Drive
	var comment string
	comment = "test comment"

	row := db().QueryRow(statement)
	err := row.Scan(&drive.Id, &drive.StartDate, &drive.EndDate, &drive.Duration, &drive.StartAddress, &drive.EndAddress, &drive.StartOdometer, &drive.EndOdometer, &drive.Distance, &drive.Classification, &drive.GroupId)
	if err != nil {
		return drive, comment, err
	}

	drive.ClassificationClass = "unknown"
	drive.ClassificationString = ""
	if drive.Classification.Valid {
		switch drive.Classification.Int32 {
		case business:
			drive.ClassificationClass = "business"
			drive.ClassificationString = "Tjänsteresa"

		case private:
			drive.ClassificationClass = "private"
			drive.ClassificationString = "Privat resa"
		}
	}

	drive.StartTime = convertTime(drive.StartDate).Format("15:04")
	drive.EndTime = convertTime(drive.EndDate).Format("15:04")

	h, m := minutesToHoursAndMinutes(drive.Duration)
	drive.DurationString = fmt.Sprintf("%d:%02d", h, m)
	drive.DistanceString = fmt.Sprintf("%.2f", drive.Distance)

	return drive, comment, nil
}

func getPositions(driveIds []string) ([]Position, error) {
	var positions []Position

	statement := `
	SELECT
	    positions.longitude,
        positions.latitude
	FROM
	    positions, drives
	WHERE
		drives.id = ANY('{`

	for _, id := range driveIds {
		statement += id + ","
	}
	statement = strings.TrimRight(statement, ",")
	statement += `}') AND 
        positions.date BETWEEN drives.start_date AND drives.end_date
	ORDER BY
	    positions.date ASC;
	`

	rows, _ := db().Query(statement)
	defer rows.Close()

	for rows.Next() {
		var pos Position

		err := rows.Scan(&pos.Longitude, &pos.Latitude)
		if err != nil {
			return nil, err
		}

		positions = append(positions, pos)
	}

	return positions, rows.Err()
}

func getCars() ([]Car, error) {
	var cars []Car

	statement := "SELECT id, model, name FROM cars ORDER BY id ASC;"

	rows, _ := db().Query(statement)
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

func getTotals(year, month, carId int) (Totals, error) {
	from := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)

	statement := fmt.Sprintf(`
    SELECT
        *,
        duration_total - (duration_business + duration_private) as duration_unknown,
        distance_total - (distance_business + distance_private) as distance_unknown
    FROM
        (SELECT
            sum(case when c.classification=1 then drives.duration_min else 0 end) as duration_business,
            sum(case when c.classification=1 then drives.distance else 0 end) as distance_business,
            sum(case when c.classification=2 then drives.duration_min else 0 end) as duration_private,
            sum(case when c.classification=2 then drives.distance else 0 end) as distance_private,
            sum(drives.duration_min) as duration_total,
            sum(drives.distance) as distance_total
        FROM drives
        LEFT JOIN tj_classifications c ON c.drive_id=drives.id
    WHERE drives.car_id=%d AND drives.start_date >= '%s'::date AND drives.start_date < '%s'::date) a`, carId, from.Format("2006-01-02 15:04:05.000"), to.Format("2006-01-02 15:04:05.000"))

	var t Totals

	row := db().QueryRow(statement)
	err := row.Scan(&t.TotalBusinessDuration, &t.TotalBusinessDistance, &t.TotalPrivateDuration, &t.TotalPrivateDistance, &t.TotalDuration, &t.TotalDistance, &t.UnclassifiedDuration, &t.UnclassifiedDistance)
	if err != nil {
		return t, err
	}

	return t, nil
}

func createTables() error {
	statement := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS public.tj_classifications
    (
        drive_id integer NOT NULL,
        classification integer,
        PRIMARY KEY (drive_id)
    );
    ALTER TABLE public.tj_classifications
    OWNER to %s;`, config.Connection.User)

	_, err := db().Exec(statement)
	if err != nil {
		return err
	}

	fmt.Println("Classifications table exists.")

	statement = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS public.tj_grouped_drives
    (
        id SERIAL PRIMARY KEY,
        car_id integer NOT NULL,
        drive_ids integer[] NOT NULL,
        start_date timestamp without time zone NOT NULL,
        end_date timestamp without time zone NOT NULL,
        start_address character varying NOT NULL,
        end_address character varying NOT NULL,
        distance double precision NOT NULL,
        duration_min smallint NOT NULL,
        classification integer,
        comment text
    );
    ALTER TABLE public.tj_grouped_drives
    OWNER to %s;`, config.Connection.User)

	_, err = db().Exec(statement)
	if err != nil {
		return err
	}

	fmt.Println("Grouped drives table exists.")

	statement = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS public.tj_comments
    (
        drive_id integer NOT NULL,
		comment text,
		PRIMARY KEY (drive_id)
    );
    ALTER TABLE public.tj_comments
    OWNER to %s;`, config.Connection.User)

	_, err = db().Exec(statement)
	if err != nil {
		return err
	}

	fmt.Println("Grouped drives table exists.")

	return nil
}
