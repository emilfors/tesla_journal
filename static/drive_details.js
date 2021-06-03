$(document).ready(function() {
    $.get("/drive/" + (group ? "group/" : "") + id,
        function(data, status) {
            var json = JSON.parse(data);

            makeMap(JSON.stringify(json.MapData));
            populateDetails(json.Drives);
        }
    );
    
    function makeMap(mapData) {
        var map = L.map('map');

        var geojsonLayer = new L.GeoJSON(jQuery.parseJSON(mapData));
        map.addLayer(geojsonLayer).fitBounds(geojsonLayer.getBounds());

        L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
            attribution: '&copy; <a href="http://osm.org/copyright">OpenStreetMap</a> contributors'
        }).addTo(map);
    }

    function populateDetails(drives) {
        $("#odometer_start").html(drives.StartOdometer);
        $("#odometer_end").html(drives.EndOdometer);
        $("#classification").html(drives.Classification);
        $("#comment").html(drives.Comment);
    }
});
