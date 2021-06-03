function reloadPage() {
    window.location = window.location.href;
}

$(document).ready(function() {
    $("body").on("hover", ".day",
        function() {
        },
        function() {
        }
    );

    $("body").on("click", ".day",
        function() {
        }
    );

    $("body").on("click", ".mastercb",
        function() {
            var checked = this.checked;
            $(this).parents(".day").find(":checkbox").not(this).each(
                function() {
                    this.checked = checked;
                }
            );

            checkedDrivesChanged();
        }
    );

    $("body").on("click", ".drivecb",
        function() {
            if (!this.checked) {
                $(this).parents(".day").find(".mastercb").each(
                    function() {
                        this.checked = false;
                    }
                );
            }

            checkedDrivesChanged();
        }
    );

    var frm = $("#dayform");
    frm.submit(function (e) {
        e.preventDefault();

        $.ajax({
            type: frm.attr("method"),
            url: frm.attr("action"),
            data: frm.serialize(),
            success: function (data) {
                var json = JSON.parse(data);

                populateTotals(json.Totals);
                populateDays(json.AffectedDays);

                checkedDrivesChanged();
            },
            error: function (data) {
                console.log('An error occurred.');
                console.log(data);
            },
        });
    });

    function checkedDrivesChanged() {
        var checkedGroupDrives = $(".groupedcb:checked").length;
        var checkedDrives = $(".drivecb:checked").not(".groupedcb").length;
        var nothingChecked = checkedDrives == 0 && checkedGroupDrives == 0;

        $("#btn_business").prop("disabled", nothingChecked);
        $("#btn_private").prop("disabled", nothingChecked);
        $("#btn_group").prop("disabled", nothingChecked || checkedDrives < 2  || checkedGroupDrives > 0);
        $("#btn_ungroup").prop("disabled", nothingChecked || checkedDrives > 0 || checkedGroupDrives == 0);
    }

    $("#btn_business").click(
        function() {
            $("#action").val("classify");
            $("#classification").val("business");

            $("#dayform").submit();
        }
    );

    $("#btn_private").click(
        function() {
            $("#action").val("classify");
            $("#classification").val("private");

            $("#dayform").submit();
        }
    );

    $("#btn_group").click(
        function() {
            $("#action").val("group");

            $("#dayform").submit();
        }
    );

    $("#btn_ungroup").click(
        function() {
            $("#action").val("ungroup");

            $("#dayform").submit();
        }
    );

    function populateTotals(totals) {
        var html = "";
        html += "Total körsträcka: " + totals.TotalDistance.toFixed(1) + " km<br>";
        html += "Varav tjänsteresor: " + totals.TotalBusinessDistance.toFixed(1) + " km<br>";
        html += "Varav privatresor: " + totals.TotalPrivateDistance.toFixed(1) + " km";
        if (totals.UnclassifiedDistance > 0) {
            html += "<br><font color='red'>Oklassificerad sträcka: " + totals.UnclassifiedDistance.toFixed(1) + " km</font>";
        }

        $("#totaldistances").html(html);

        html = "";
        html += "Total tid: " + tohhmm(totals.TotalDuration) + "<br>";
        html += "Varav tjänsteresor: " + tohhmm(totals.TotalBusinessDuration) + "<br>";
        html += "Varav privatresor: " + tohhmm(totals.TotalPrivateDuration);
        if (totals.UnclassifiedDuration > 0) {
            html += "<br><font color='red'>Oklassificerad tid: " + tohhmm(totals.UnclassifiedDuration) + "</font>";
        }

        $("#totaldurations").html(html);
    }

    function tohhmm(minutes) {
        var mm = minutes;
        var hh = 0;

        while (mm >= 60) {
            hh += 1;
            mm -= 60;
        }

        return hh + ":" + (mm).toLocaleString(undefined, {minimumIntegerDigits: 2});
    }

    function populateDays(days) {
        $.each(days,
            function(i, day) {
                populateDay(day);
            }
        );
    }

    function populateDay(day) {
        var html = "";

        html += "<tr height=10>";
        html += "<td align=left width=25>";
        html += "    <input type='checkbox' class='mastercb'/>";
        html += "</td>";

        html += "<td width=25>";
        html += "    &nbsp;";
        html += "</td>";

        html += "<td align=left colspan=5>";
        html += "    <span class='date'>" + day.DateString + "</span>";
        html += "</td>";
        html += "</tr>";

        html += "<tr height=10>";
        html += "<td colspan=7>";
        html += "&nbsp;";
        html += "</td>";
        html += "</tr>";

        html += makeDrivesHTML(day.Drives, day.GroupedDrives);

        $("#day_" + day.DateAsTs).html(html);
    }

    function makeDrivesHTML(drives, groupedDrives) {
        var html = "";

        var currentGroupId = -1
        $.each(drives,
            function(i, drive) {
                var gid = drive.GroupId.Valid ? drive.GroupId.Int32 : -1;

                if (gid != -1 && gid != currentGroupId) {
                    currentGroupId = gid;

                    html += makeDriveHTML(getGroupedDrive(groupedDrives, gid), gid);
                }

                if (gid == -1) {
                    html += makeDriveHTML(drive);
                }
            }
        );

        return html;
    }

    function makeDriveHTML(drive, groupID = -1) {
        var html = "";

        var endpoint = "details/";

        html += "<tr>";
        html += "<td align=left valign=center width=25>";
        if (groupID != -1) {
            endpoint = "group" + endpoint + groupID;
            html += "<input type='checkbox' class='drivecb groupedcb' name='groupeddrive' value='" + groupID + "'/>";
        } else {
            endpoint = endpoint + drive.Id;
            html += "    <input type='checkbox' class='drivecb' name='drive' value='" + drive.Id + "'/>";
        }
        html += "</td>";

        html += "<td align=center valign=center width=25>";
        if (groupID != -1) {
            html += "<a href='" + endpoint + "'>";
            html += "<svg width='24' height='30'>";
            html += "<use x='0' y='0' xlink:href='#merge'/>";
            html += "</svg>";
            html += "</a>";
        } else {
            html += "    &nbsp;";
        }
        html += "</td>";

        html += "<td align=left width=250>";
        html += "    <span lang=sv style='font-size: 10.0pt; font-family:Calibri;'>";
        html += "    <a href='" + endpoint + "'>";
        html += "    " + drive.EndAddress + "<br>";
        html += "    " + drive.StartAddress;
        html += "    </a>";
        html += "    </span>";
        html += "</td>";

        html += "<td align=right width=50>";
        html += "    <span lang=sv style='font-size: 10.0pt;font-family:Calibri;'>";
        html += "    <a href='" + endpoint + "'>";
        html += "    " + drive.EndTime + "<br>";
        html += "    " + drive.StartTime;
        html += "    </a>";
        html += "    </span>";
        html += "</td>";

        html += "<td width=150>";
        html += "    &nbsp;";
        html += "</td>";

        html += "<td align=left width=250>";
        html += "    <span lang=sv style='font-size: 10.0pt;font-family:Calibri;'>";
        html += "    <a href='" + endpoint + "'>";
        html += "    Körsträcka: " + drive.DistanceString + " km<br>";
        html += "    Tid: " + drive.DurationString;
        html += "    </a>";
        html += "    </span>";
        html += "</td>";

        html += "<td class=" + drive.ClassificationClass + " align=right width=150>";
        html += "    <a class=" + drive.ClassificationClass + " href='" + endpoint + "'>" + drive.ClassificationString + "</a>";
        html += "</td>";
        html += "</tr>";

        html += "<tr height=10>";
        html += "<td colspan=7>";
        html += "&nbsp;";
        html += "</td>";
        html += "</tr>";

        return html;
    }

    function getGroupedDrive(groupedDrives, gid) {
        for (var i = 0; i < groupedDrives.length; i++) {
           if (groupedDrives[i].Id == gid) {
              return groupedDrives[i];
           }
        }

        return null;
    }

    function makeMap() {
        var mymap = L.map('map').setView([51.505, -0.09], 13);

        var feature = {
            "type": "Feature",
            "properties": {
                "style": {
                    "color": "#004070",
                    "weight": 4,
                    "opacity": 1
                }
            },
            "geometry": {
                "type": "MultiPoint",
                "coordinates": [
                    [0.25, 51.47],
                    [0.26, 51.47],
                    [0.27, 51.47]
                ]
            }
        };
        var geojsonLayer = new L.GeoJSON(feature);
        map.addLayer(geojsonLayer).fitBounds(geojsonLayer.getBounds());

        L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
            attribution: '&copy; <a href="http://osm.org/copyright">OpenStreetMap</a> contributors'
        }).addTo(map);
    }
});
