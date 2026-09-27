package models

import "time"

// The block types below describe what a route reports, not what it decodes. Every
// member is a pointer, because a value the upstream genuinely measured as 0 has
// to serialise as 0, and only a member the upstream had no value for may
// serialise as null. Most members carry no omitempty. A pointer member may carry
// omitempty safely, since it then drops only nil and can never discard a measured
// zero; the precipitation blocks use it so that a window the route's endpoint does
// not document is omitted instead of reported as null.
//
// A block that is itself absent is nil, which is a third state distinct from
// both: a block of nulls would claim the upstream reported the block and found
// every member missing.
//
// The upstream structs in openweather.go cannot serve this purpose. They are
// decode targets whose zero values are correct, and several of them carry
// omitempty, which drops a measured 0 on the floor.
//
// CurrentWeatherPayload and ForecastPayload, the last two types in this file, are
// the decode-side types among them: pointer-shaped views of the same bodies, kept
// beside the upstream structs they complement and the schemas they feed. Their own
// doc comments say why they have to exist.

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

// RainBlock represents the rain block of the /data/2.5 endpoints. OneHour is a
// documented window, so it carries no omitempty: an absent reading reports null
// and a block upstream reported with no window in it does not look like a block
// that was never sent. ThreeHour is the window /data/2.5/weather does not
// document, so it is tagged omitempty, which on a pointer field drops only nil and
// can never discard a measured zero. /data/2.5/forecast does not document OneHour
// at all, so it uses ForecastRainBlock instead of this type.
type RainBlock struct {
	OneHour   *float64 `json:"1h"`
	ThreeHour *float64 `json:"3h,omitempty"`
}

// SnowBlock represents the snow block of the /data/2.5 endpoints. It tags its
// members for the same reasons as RainBlock: OneHour has no omitempty because the
// window is documented, and ThreeHour has it because this endpoint does not
// document that window. /data/2.5/forecast uses ForecastSnowBlock.
type SnowBlock struct {
	OneHour   *float64 `json:"1h"`
	ThreeHour *float64 `json:"3h,omitempty"`
}

// ForecastRainBlock is the rain block of the /data/2.5/forecast slots and of the
// day rolled up from them.
//
// It exists because the two forecast endpoints need opposite treatment of the
// same two windows and one shared type cannot express both. /data/2.5/weather
// documents 1h and not 3h, so RainBlock makes 1h a required key that reports null
// when the upstream measured nothing: a body of "rain":{} has to read as
// {"1h":null} rather than as {}. /data/2.5/forecast documents 3h and not 1h, so
// the 1h key must be absent entirely rather than permanently null, and a key the
// endpoint does not document carries no information. Omitting the member is the
// only way to say that with a struct tag.
//
// ThreeHour is a pointer with no omitempty, because the window is documented here:
// a measured 0 serialises as 0, a block upstream sent with no window in it reports
// null, and a block upstream never sent is a nil block, which is three different
// readings.
type ForecastRainBlock struct {
	ThreeHour *float64 `json:"3h"`
}

// ForecastSnowBlock is the snow block of the /data/2.5/forecast route, and exists
// for exactly the reason ForecastRainBlock does: the 1h window is not documented
// here, so the member is absent rather than null, while 3h is documented and
// reports null, 0 or an absent block as three separate states.
type ForecastSnowBlock struct {
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
// timezone is a pointer to match the pointer-shaped payload it is read from, so
// the faithful schema states one nullability rule rather than two.
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

// CurrentWeatherPayload is a pointer-shaped mirror of the /data/2.5/weather body
// and the authority on which members the upstream actually sent. It declares the
// conditional members the mapper cannot recover from OpenWeatherMapResponse: the
// main, wind, clouds and sys blocks as pointers, main.sea_level,
// main.grnd_level, main.temp_kf, wind.gust, the rain and snow blocks with their
// 1h windows, and timezone.
//
// It exists beside OpenWeatherMapResponse because that struct is a non-pointer
// decode target, where a member the upstream measured as 0 and a member it never
// sent are the same zero. A response that has to report the difference cannot be
// built from it, so the body is decoded twice from the same already-fetched bytes:
// the mapper takes values from the upstream struct and presence from here. A newly
// documented upstream field has to be added to both types, because a field missing
// from this one is reported as null even when the upstream did send it.
// TestCurrentPayloadCoversDecodedFields is the guard on that, and its allowlist
// names every JSON member this type deliberately narrows.
//
// Precipitation is taken from here and not from owm: models.Rain and models.Snow
// are still decoded on every fetch and read by nobody, because their non-pointer
// windows cannot tell a measured zero from an absent one. Do not read them, and do
// not add a third copy of them here.
//
// timezone is bound here and nowhere else. OpenWeatherMapResponse does not carry
// it because no legacy field is derived from it, and an unused decode field is
// worse than a missing one.
type CurrentWeatherPayload struct {
	Main *currentPayloadMain `json:"main"`
	Wind *currentPayloadWind `json:"wind"`
	// Clouds and Sys carry no members on purpose. Every member they report is
	// unconditionally sent, so the mapper reads those values from
	// OpenWeatherMapResponse and needs only the pointer to know the block was
	// there at all.
	Clouds *struct{}             `json:"clouds"`
	Sys    *struct{}             `json:"sys"`
	Rain   *currentPayloadPrecip `json:"rain"`
	Snow   *currentPayloadPrecip `json:"snow"`
	// Timezone is the UTC offset the upstream reports, in seconds.
	Timezone *int `json:"timezone"`
}

// currentPayloadMain holds the main block members that are conditional rather
// than guaranteed: sea_level and grnd_level come only from points near sea level
// or the ground, and temp_kf is not always sent. The rest of main is read from
// OpenWeatherMapResponse.
type currentPayloadMain struct {
	SeaLevel  *int     `json:"sea_level"`
	GrndLevel *int     `json:"grnd_level"`
	TempKF    *float64 `json:"temp_kf"`
}

// currentPayloadWind holds wind.gust, which some regions report and others omit.
// speed and deg are read from OpenWeatherMapResponse.
type currentPayloadWind struct {
	Gust *float64 `json:"gust"`
}

// currentPayloadPrecip holds the 1h window both rain and snow report on this
// endpoint. A nil block means upstream sent no block at all, which is not the
// same as a block whose window measures 0. The 3h window is not declared here
// because /data/2.5/weather does not document it; see RainBlock.
type currentPayloadPrecip struct {
	OneHour *float64 `json:"1h"`
}

// TempPoint is a time of day temperature breakdown. /data/3.0/onecall fills every
// member it documents. /data/2.5/forecast has no breakdown at all: it fills only
// Min and Max there, and those two are derived by the mapper from the day's slots
// rather than measured by the upstream. The rest stay null, because a value the
// endpoint cannot supply is not a zero.
type TempPoint struct {
	Day   *float64 `json:"day"`
	Min   *float64 `json:"min"`
	Max   *float64 `json:"max"`
	Night *float64 `json:"night"`
	Morn  *float64 `json:"morn"`
	Eve   *float64 `json:"eve"`
}

// FeelsLikePoint is the apparent temperature breakdown. Every member is null on
// /forecast, which reports no breakdown; /forecast/7day fills all four.
type FeelsLikePoint struct {
	Day   *float64 `json:"day"`
	Night *float64 `json:"night"`
	Morn  *float64 `json:"morn"`
	Eve   *float64 `json:"eve"`
}

// ForecastSlot is one raw three hour slot from the list[] array, carried on the day
// it belongs to so a client can read a single period instead of a daily rollup.
// The members the upstream did not send are null, so a slot that measures a zero
// and a slot that was never told about the quantity are different readings.
//
// Sys is the one place a response type here points at an upstream struct rather
// than a pointer shaped one: pod is a string the endpoint sends whenever the sys
// block is there, so the pointer on this field is the whole of the presence
// signal, and a second pointer for a string the endpoint does document would add
// nothing.
//
// Rain and Snow use the forecast precipitation types rather than the shared ones,
// so the 1h window is absent here rather than null: this endpoint documents the
// 3h window only, and a key it does not document carries no information. See
// ForecastRainBlock for why the two routes cannot share one type.
type ForecastSlot struct {
	Dt         int64              `json:"dt"`
	Main       *MainBlock         `json:"main"`
	Weather    []Weather          `json:"weather"`
	Clouds     *CloudsBlock       `json:"clouds"`
	Wind       *WindBlock         `json:"wind"`
	Visibility *int               `json:"visibility"`
	Pop        *float64           `json:"pop"`
	Rain       *ForecastRainBlock `json:"rain"`
	Snow       *ForecastSnowBlock `json:"snow"`
	Sys        *ForecastSys       `json:"sys"`
	DtTxt      string             `json:"dt_txt"`
}

// ForecastDay is one entry of the forecast route's forecast[] array, in the
// upstream field order of /data/2.5/forecast rolled up to a day, carrying the
// legacy vocabulary alongside it.
//
// Three JSON keys are wanted by both vocabularies and two Go fields cannot share
// one tag, so one field serves each pair. Date is the day the slots fell on.
// Humidity is the day's mean, which is what legacy Forecast.Humidity has always
// been. WindSpeed is the day's mean, which is what legacy Forecast.WindSpeed has
// always been. Their JSON is byte identical to the response this type replaces.
//
// Every numeric key here is a pointer, precipitation excepted and explained below,
// because the route cannot always produce one: a day whose slots reported no main
// block has no temperature to report, and three fabricated zeroes beside a null temp
// block is worse than saying so. For a real body the JSON is byte identical, because
// a non-nil pointer to a value serialises as that value.
//
// ChanceOfRain is null when no slot carried pop, UVIndex is always null because the
// three hour endpoint reports no uvi, and Pressure is null when no slot carried a
// main block.
type ForecastDay struct {
	Date        time.Time `json:"date"`
	MaxTemp     *float64  `json:"max_temperature"`
	MinTemp     *float64  `json:"min_temperature"`
	AvgTemp     *float64  `json:"avg_temperature"`
	Condition   string    `json:"condition"`
	Description string    `json:"description"`
	Icon        string    `json:"icon"`
	// Humidity truncates its daily mean while Pressure, Clouds and Visibility round
	// theirs, and the split is deliberate. Humidity's legacy counterpart is an int
	// field that has always truncated, so rounding it would change a number existing
	// consumers already read and buy no accuracy: a mean of 67.5 percent is 67 either
	// way as far as a legacy int key is concerned. The other three have no legacy
	// counterpart, so nothing constrains them and they carry the nearest integer.
	//
	// Precipitation is the one numeric key that stays a value: a total of zero is a
	// real answer for a day nothing fell on, not an absence, and the legacy key has
	// always been a number.
	Humidity      *int     `json:"humidity"`
	WindSpeed     *float64 `json:"wind_speed"`
	Precipitation float64  `json:"precipitation"`
	ChanceOfRain  *int     `json:"chance_of_rain"`
	UVIndex       *float64 `json:"uv_index"`

	// Pop is the highest probability any slot of the day reported, PopMin the
	// lowest and PopMean the mean of the slots that carried one. All three are null
	// when no slot reported a probability, which is a different statement from a day
	// whose probability was measured as zero.
	//
	// Temp.Min and Temp.Max are derived: the extremes of the day's slot
	// temperatures. They are not the same quantity as a slot's main.temp_min and
	// main.temp_max, which the upstream measures over its own three hour window.
	//
	// Clouds, Visibility, Pressure, WindDeg, WindGust, Rain and Snow are the day's
	// rollup as well. WindDeg is the direction of the slot with the day's strongest
	// wind, matching the documented meaning of deg on the daily endpoints. Rain and
	// Snow are sums of the slots' 3h windows and are null when no slot reported one.
	//
	// FeelsLike and Uvi are always null here: the three hour endpoint reports
	// neither.
	Pop        *float64           `json:"pop"`
	PopMin     *float64           `json:"pop_min"`
	PopMean    *float64           `json:"pop_mean"`
	Temp       *TempPoint         `json:"temp"`
	FeelsLike  *FeelsLikePoint    `json:"feels_like"`
	WindDeg    *int               `json:"wind_deg"`
	WindGust   *float64           `json:"wind_gust"`
	Clouds     *int               `json:"clouds"`
	Visibility *int               `json:"visibility"`
	Pressure   *int               `json:"pressure"`
	Rain       *ForecastRainBlock `json:"rain"`
	Snow       *ForecastSnowBlock `json:"snow"`
	Uvi        *float64           `json:"uvi"`
	Weather    []Weather          `json:"weather"`
	Hourly     []ForecastSlot     `json:"hourly"`
}

// ForecastResponse is the body of GET|POST /api/v1/weather/forecast, served inside
// the {"success":true,"data":{...}} envelope.
//
// The first group mirrors the /data/2.5/forecast envelope, in upstream field order.
// cod is a string on this endpoint and message is the float the upstream sends, the
// calculation time, so neither is an int. cnt is the integer the decode struct
// binds. None of their zeroes is a reading a real response makes, so all three are
// value fields. City is a pointer because the block can be absent.
//
// The second group is the legacy vocabulary, unchanged, so existing consumers keep
// working. Forecast is a slice with no omitempty, so the key is always present even
// when the upstream sent no slots.
type ForecastResponse struct {
	Cod     string  `json:"cod"`
	Message float64 `json:"message"`
	Cnt     int     `json:"cnt"`
	City    *City   `json:"city"`

	Location    Location      `json:"location"`
	Current     Current       `json:"current"`
	Forecast    []ForecastDay `json:"forecast"`
	RequestTime time.Time     `json:"request_time"`
}

// ForecastPayload is a pointer-shaped mirror of the /data/2.5/forecast body and the
// authority on which members the upstream actually sent. It declares the
// conditional members the mapper cannot recover from
// OpenWeatherMapForecastResponse: per slot, the main, wind, clouds, rain, snow and
// sys blocks as pointers, main.sea_level, main.grnd_level, wind.gust, the rain and
// snow 3h windows, and pop and visibility; and at the envelope level, the city
// block.
//
// It exists beside OpenWeatherMapForecastResponse because models.ForecastItem and
// the blocks it holds are non-pointer decode targets, where a member the upstream
// measured as 0 and a member it never sent are the same zero. A response that has
// to report the difference cannot be built from them, so the body is decoded twice
// from the same already-fetched bytes: the mapper takes values from the upstream
// struct and presence from here. That is one upstream request, not two. A newly
// documented upstream field has to be added to both types, because a field missing
// from this one is reported as absent even when the upstream did send it.
// TestForecastPayloadCoversDecodedFields is the guard on that, and its allowlist
// names every JSON member this type deliberately narrows.
//
// The two list[] decodes see the same array in the same order, so index i of List
// is the presence view of index i of OpenWeatherMapForecastResponse.List. A body
// whose list is longer than the payload's cannot happen: both decodes walk one
// array, and a member one of them cannot type fails the request rather than
// shortening it.
//
// pop is treated as conditional because the endpoint does not send it on every
// slot, and the daily rollup excludes the slots that carry none from pop, pop_min
// and pop_mean alike.
type ForecastPayload struct {
	// City carries no members on purpose. Every member the block reports is
	// unconditionally sent, so the mapper reads those values from the upstream
	// struct and needs only the pointer to know the block was there at all.
	City *struct{}             `json:"city"`
	List []ForecastPayloadItem `json:"list"`
}

// ForecastPayloadItem is the presence view of one three hour slot. Its members are
// the slots' conditional ones; dt, dt_txt, weather, the always sent members of
// main, wind and clouds, and sys.pod are read from the upstream struct.
type ForecastPayloadItem struct {
	Main       *ForecastPayloadMain   `json:"main"`
	Wind       *ForecastPayloadWind   `json:"wind"`
	Clouds     *struct{}              `json:"clouds"`
	Rain       *ForecastPayloadPrecip `json:"rain"`
	Snow       *ForecastPayloadPrecip `json:"snow"`
	Sys        *struct{}              `json:"sys"`
	Pop        *float64               `json:"pop"`
	Visibility *int                   `json:"visibility"`
}

// ForecastPayloadMain holds the main block members of a slot that are conditional
// rather than guaranteed: sea_level and grnd_level come only from points near sea
// level or the ground. temp_kf is not declared here because /data/2.5/forecast does
// not document it for a slot, so the response reports it null.
type ForecastPayloadMain struct {
	SeaLevel  *int `json:"sea_level"`
	GrndLevel *int `json:"grnd_level"`
}

// ForecastPayloadWind holds wind.gust on a slot, which some regions report and
// others omit. speed and deg are read from the upstream struct.
type ForecastPayloadWind struct {
	Gust *float64 `json:"gust"`
}

// ForecastPayloadPrecip holds the 3h window both rain and snow report on this
// endpoint. A nil block means the upstream sent no block at all, which is not the
// same as a block whose window measures 0. The 1h window is not declared here
// because /data/2.5/forecast does not document it; see RainBlock.
type ForecastPayloadPrecip struct {
	ThreeHour *float64 `json:"3h"`
}
