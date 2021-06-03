# Tesla Journal

## Build and install

Prerequisites:

* A [correctly configured](https://golang.org/doc/install#testing) Go toolchain
* A [set up and working](https://docs.teslamate.org/docs/installation/debian) Teslamate installation
* If you intend to run the Tesla Journal service on a different host than the one Teslamate runs on, you need to
  configure PostgreSQL to allow remote connections

Build Tesla Journal:
```sh
go get
go build
```

Installation:
Edit the configuration file `tesla_journal.cfg`. Set the parameters to suitable values for your system. If you run Tesla Journal
on the same host as Teslamate and haven't changed the default database connection values, it should be enough to set the Password
parameter to your PostgreSQL password.

If you wish to secure (https) connections to Tesla Journal you can generate a self-signed TSL certificate. Use the parameters `CertFile` and
`KeyFile` in the configuration file to point out your certificate files. If those parameters are omitted, the service will accept
unsecured (http) connections only.

```cfg
[Connection]
Host = "localhost"
Port = 5432
User = "teslamate"
Password = "your_db_password"
DB = "teslamate"

[Service]
Port = 4001
CertFile = "your_certificate.crt"
KeyFile = "your_certificate.key"
```

Create a file named `tesla_journal.service` in `/etc/systemd/system/`, with the following contents (edit the paths to match your needs):
```sh
[Unit]
Description=TeslaJournal
After=teslamate

[Service]
Type=simple

Restart=always
RestartSec=5

WorkingDirectory=/path/to/tesla_journal/

ExecStart=/path/to/tesla_journal/tesla_journal

[Install]
WantedBy=multi-user.target
```
Start the service:
```sh
sudo systemctl start tesla_journal
```
Automatically start the service on boot:
```sh
sudo systemctl enable tesla_journal
```

## Usa Tesla Journal

Connect to the service using a web browser. Use the drop-down selection boxes for picking the car and month you want to work with.
The page will display a list of the days in the month you selected, each day listing the drives you did that day.

The drives can be classified as business or private trips. You can also group two or more trips together. Groups can be ungrouped
if you wish to see their individual drives. To perform an action on the drives, select them using their checkboxes and press the action button.

## Known problems

There are currently no known problems.

## Plans for future development

* Adding comments to drives
* Document generation (PDF?)

## License

MIT license. See the LICENSE file for details.
