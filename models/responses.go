package models

import "time"

// The block types below describe what a route reports, not what it decodes. Every
// member is a pointer and none carries omitempty, because a value the upstream
// genuinely measured as 0 has to serialise as 0, and only a member the upstream
// had no value for may serialise as null. A block that is itself absent is nil,
// which is a third state distinct from both: a block of nulls would claim the
// upstream reported the block and found every member missing.
//
// The upstream structs in openweather.go cannot serve this purpose. They are
// decode targets whose zero values are correct, and several of them carry
// omitempty, which drops a measured 0 on the floor.

// MainBlock represents the main block of the /data/2.5 endpoints. sea_level and
// grnd_level are reported only for points near sea level or the ground, so they
// are null everywhere else.
type MainBlock struct {
	Temp      *float64 `json:"temp"`
	FeelsLike *float64 `json:"feels_like"`
	TempMin   *float64 `json:"temp_min"`
	TempMax   *float64 `json:"temp_max"`
	Pressure  *int     `json:"pressure"`
	Humidity  *int     `json:"humidity"`
	SeaLevel  *int     `json:"sea_level"`
	GrndLevel *int     `json:"grnd_level"`
	TempKF    *float64 `json:"temp_kf"`
}

// WindBlock represents the wind block of the /data/2.5 endpoints. gust is
// reported by some regions and not by others.
type WindBlock struct {
	Speed *float64 `json:"speed"`
	Deg   *int     `json:"deg"`
	Gust  *float64 `json:"gust"`
}

// CloudsBlock represents the clouds block of the /data/2.5 endpoints.
type CloudsBlock struct {
	All *int `json:"all"`
}

// RainBlock represents the rain block of the /data/2.5 endpoints. It carries both
// documented windows because the block type is shared between routes:
// /data/2.5/weather reports 1h and /data/2.5/forecast reports 3h. The window a
// route's endpoint does not document stays null.
type RainBlock struct {
	OneHour   *float64 `json:"1h"`
	ThreeHour *float64 `json:"3h"`
}

// SnowBlock represents the snow block of the /data/2.5 endpoints. It carries both
// documented windows for the same reason as RainBlock.
type SnowBlock struct {
	OneHour   *float64 `json:"1h"`
	ThreeHour *float64 `json:"3h"`
}

// SysBlock represents the sys block of the /data/2.5 endpoints.
type SysBlock struct {
	Type    *int    `json:"type"`
	ID      *int    `json:"id"`
	Country *string `json:"country"`
	Sunrise *int64  `json:"sunrise"`
	Sunset  *int64  `json:"sunset"`
}

// CurrentWeatherResponse is the body of GET|POST /api/v1/weather/current, served
// inside the {"success":true,"data":{...}} envelope.
//
// The first group is a faithful mirror of the /data/2.5/weather schema, in
// upstream field order. coord and weather reuse the upstream types, which carry
// no omitempty and are fully populated whenever that endpoint answers at all.
// visibility, dt, id, cod, base and name are always sent by that endpoint, so
// they are plain values: none of their zeroes is a reading a real response makes.
// timezone is a pointer because models.OpenWeatherMapResponse has no field for
// it; the service binds it from the payload, so it is null only if the upstream
// omits it.
//
// The second group is the legacy vocabulary, unchanged, so existing consumers keep
// working. The legacy blocks keep their own omitempty, which means a legacy key
// whose value is zero can still be absent while its faithful twin reports the
// zero.
//
// /data/2.5/weather has no pop, so no chance_of_rain key is emitted here in either
// vocabulary. The legacy Current struct has never carried one, and a key that
// could only ever be null would carry no information.
type CurrentWeatherResponse struct {
	Coord      Coordinates  `json:"coord"`
	Weather    []Weather    `json:"weather"`
	Base       string       `json:"base"`
	Main       *MainBlock   `json:"main"`
	Visibility int          `json:"visibility"`
	Wind       *WindBlock   `json:"wind"`
	Clouds     *CloudsBlock `json:"clouds"`
	Rain       *RainBlock   `json:"rain"`
	Snow       *SnowBlock   `json:"snow"`
	Dt         int64        `json:"dt"`
	Sys        *SysBlock    `json:"sys"`
	ID         int          `json:"id"`
	Timezone   *int         `json:"timezone"`
	Name       string       `json:"name"`
	Cod        int          `json:"cod"`

	Location    Location  `json:"location"`
	Current     Current   `json:"current"`
	RequestTime time.Time `json:"request_time"`
}
