# Weathering with Go 🌤️

A modern, high-performance weather API built with Go and the Gin web framework. Get current weather conditions and forecasts for any location worldwide using the OpenWeatherMap API.

## ✨ Features

- **Current Weather**: Get real-time weather data for any location
- **Weather Forecasts**: 5-day weather forecasts with 3-hour intervals, plus a 7-day daily forecast
- **Response Caching**: 15-minute in-memory cache to protect your OpenWeatherMap quota
- **Multiple Units**: Support for metric, imperial, and Kelvin units
- **RESTful API**: Clean, well-documented REST endpoints
- **Error Handling**: Comprehensive error handling with detailed responses
- **Rate Limiting**: Built-in protection against API abuse
- **CORS Support**: Cross-origin resource sharing enabled
- **Health Checks**: Monitor API health and status
- **Middleware**: Security headers, logging, and request tracking

## 🚀 Quick Start

### Prerequisites

- Go 1.25 or higher
- OpenWeatherMap API key (free at [openweathermap.org](https://openweathermap.org/api))

### Installation

1. **Clone the repository**
   ```bash
   git clone https://github.com/tea-LZL/weathering-with-go.git
   cd weathering-with-go
   ```

2. **Install dependencies**
   ```bash
   go mod download
   ```

3. **Set environment variables**
   ```bash
   export OPENWEATHERMAP_API_KEY="your_api_key_here"
   export PORT="8080"  # Optional, defaults to 8080
   ```

4. **Run the server**
   ```bash
   go run main.go
   ```

The API will be available at `http://localhost:8080`

## 📖 API Documentation

### Base URL
```
http://localhost:8080/api/v1
```

### Authentication
No authentication required. The OpenWeatherMap API key is configured server-side, and a request may
override it (see [API key per request](#api-key-per-request)).

### Endpoints

#### GET /health
Health check endpoint to verify the API is running.

**Response:**
```json
{
  "status": "healthy",
  "service": "weathering-with-go"
}
```

#### GET /weather/current
Get current weather for a location.

**Parameters:**
- `location` (required): City name, state code, and country code (e.g., "London,UK" or "New York,NY,US")
- `units` (optional): Temperature units - `metric` (default), `imperial`, or `kelvin`

**Example:**
```bash
curl "http://localhost:8080/api/v1/weather/current?location=London,UK&units=metric"
```

**Response:**
```json
{
  "success": true,
  "data": {
    "location": {
      "name": "London",
      "country": "GB",
      "latitude": 51.5074,
      "longitude": -0.1278
    },
    "current": {
      "temperature": 15.5,
      "feels_like": 14.8,
      "humidity": 72,
      "pressure": 1013.2,
      "wind_speed": 3.6,
      "wind_direction": 230,
      "condition": "Clouds",
      "description": "Scattered Clouds",
      "icon": "03d",
      "cloud_cover": 40,
      "last_updated": "2025-09-21T10:30:00Z"
    },
    "request_time": "2025-09-21T10:30:15Z"
  }
}
```

#### POST /weather/current
Get current weather using JSON request body.

**Request Body:**
```json
{
  "location": "London,UK",
  "units": "metric"
}
```

**Response:** Same as GET endpoint

#### GET /weather/forecast
Get weather forecast for a location.

**Parameters:**
- `location` (required): City name, state code, and country code
- `units` (optional): Temperature units - `metric` (default), `imperial`, or `kelvin`
- `days` (optional): Number of forecast days (1-5, default: 5)

**Example:**
```bash
curl "http://localhost:8080/api/v1/weather/forecast?location=Tokyo,JP&units=metric&days=3"
```

**Response:**
```json
{
  "success": true,
  "data": {
    "location": {
      "name": "Tokyo",
      "country": "JP",
      "latitude": 35.6762,
      "longitude": 139.6503
    },
    "current": {
      "temperature": 19.4,
      "feels_like": 18.9,
      "humidity": 68,
      "pressure": 1011.0,
      "visibility": 0,
      "wind_speed": 4.2,
      "wind_direction": 180,
      "condition": "Clouds",
      "description": "Scattered Clouds",
      "icon": "03d",
      "max_temperature": 22.1,
      "min_temperature": 16.4,
      "cloud_cover": 40,
      "last_updated": "2025-09-21T12:00:00Z"
    },
    "forecast": [
      {
        "date": "2025-09-22T00:00:00Z",
        "max_temperature": 24.5,
        "min_temperature": 18.2,
        "avg_temperature": 21.3,
        "condition": "Clear",
        "description": "Clear Sky",
        "icon": "01d",
        "humidity": 65,
        "wind_speed": 2.1,
        "precipitation": 0,
        "chance_of_rain": 0
      }
    ],
    "request_time": "2025-09-21T10:30:15Z"
  }
}
```

The `current` block is derived from the 3-hour slot nearest to now in the same upstream response, so
it costs no extra OpenWeatherMap call. Two consequences: it can be up to 3 hours old, which is why
`last_updated` is the slot timestamp rather than the request time, and `visibility` is always `0`
because the 5-day endpoint does not report it. Use `/weather/current` when you need a live reading
or visibility.

#### POST /weather/forecast
Get weather forecast using JSON request body.

**Request Body:**
```json
{
  "location": "Tokyo,JP",
  "units": "metric",
  "days": 3
}
```

**Response:** Same as GET endpoint

#### GET /weather/forecast/7day
Get a 7-day daily forecast. Backed by the OpenWeatherMap **One Call 3.0** API, which needs its own
"One Call by Call" subscription (separate from the free 5-day/3-hour plan). The location name is
geocoded first, so each uncached request costs one Geocoding call plus one One Call call.

**Parameters:**
- `location` (required): City name, state code, and country code
- `units` (optional): Temperature units - `metric` (default), `imperial`, or `kelvin`

**Example:**
```bash
curl -i "http://localhost:8080/api/v1/weather/forecast/7day?location=Tokyo,JP&units=metric"
```

**Response:**
```json
{
  "success": true,
  "data": {
    "location": {
      "name": "Tokyo",
      "country": "JP",
      "region": "Tokyo",
      "latitude": 35.6895,
      "longitude": 139.6917
    },
    "current": {
      "temperature": 16.2,
      "feels_like": 15.8,
      "humidity": 61,
      "pressure": 1014.0,
      "visibility": 10000,
      "wind_speed": 3.1,
      "wind_direction": 190,
      "condition": "Clouds",
      "description": "Scattered Clouds",
      "icon": "03d",
      "max_temperature": 0,
      "min_temperature": 0,
      "cloud_cover": 40,
      "last_updated": "2026-03-01T06:00:00Z"
    },
    "forecast": [
      {
        "date": "2026-03-01T12:00:00Z",
        "max_temperature": 20.0,
        "min_temperature": 10.0,
        "avg_temperature": 15.0,
        "condition": "Clouds",
        "description": "Scattered Clouds",
        "icon": "03d",
        "humidity": 60,
        "wind_speed": 4,
        "precipitation": 1.5,
        "chance_of_rain": 25,
        "uv_index": 3.5
      }
    ],
    "request_time": "2026-03-01T06:00:00Z"
  }
}
```

The `current` block comes from the `current` object in the same One Call response, so it is genuinely
current and also costs no extra call. One Call reports no daily extremes in that block, so
`max_temperature` and `min_temperature` are always `0` here.

#### POST /weather/forecast/7day
Same as the GET endpoint, with a JSON body. `days` is not accepted: this route always returns 7 days.

```bash
curl -X POST "http://localhost:8080/api/v1/weather/forecast/7day" \
  -H "Content-Type: application/json" \
  -d '{"location":"Tokyo,JP","units":"metric"}'
```

### API key per request

The server-side key is used by default, but any weather route accepts a caller-supplied key:

| Where | How |
|-------|-----|
| All weather routes | `X-API-Key: <key>` header |
| GET routes | `?key=<key>` query parameter |
| POST routes | `"keys": "<key>"` in the JSON body |

### Caching

Weather responses are cached in memory for 15 minutes to keep OpenWeatherMap usage inside the free
tier quota.

- Cache keys include the api key actually used for the request (hashed), so each key has its own
  entries and a caller's key is never used to serve another caller's request.
- Concurrent identical requests collapse into a single upstream call, so a cold-cache burst costs
  one call, not one per request.
- Only successful responses are cached; errors are retried on the next request.
- Every weather response carries `X-Cache: HIT` or `X-Cache: MISS` so you can verify the savings.
- A cache hit returns the original `request_time`, which is when the data was actually fetched.
- The cache lives in the process, so every running instance has its own (relevant on Cloud Run).

### Error Responses

All errors follow a consistent format:

```json
{
  "success": false,
  "error": {
    "error": "Bad Request",
    "code": 400,
    "message": "Location is required"
  }
}
```

## ⚙️ Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `OPENWEATHERMAP_API_KEY` | Yes | - | Your OpenWeatherMap API key |
| `PORT` | No | `8080` | Server port |
| `HOST` | No | `0.0.0.0` | Server host |
| `ENVIRONMENT` | No | `development` | Environment (development/production) |
| `LOG_LEVEL` | No | `info` | Log level (debug/info/warn/error) |

### Example .env file
```env
OPENWEATHERMAP_API_KEY=your_api_key_here
PORT=8080
HOST=0.0.0.0
ENVIRONMENT=development
LOG_LEVEL=info
```

## 🏗️ Project Structure

```
weathering-with-go/
├── config/
│   └── config.go           # Configuration management
├── handlers/
│   ├── routes.go          # Route definitions
│   └── weather.go         # Weather request handlers
├── middleware/
│   └── middleware.go      # HTTP middleware
├── models/
│   ├── openweather.go     # OpenWeatherMap API models
│   └── weather.go         # Internal data models
├── services/
│   ├── cache.go           # 15 minute response cache with request collapsing
│   └── weather.go         # Weather service logic
├── utils/
│   └── errors.go          # Error handling utilities
├── go.mod                 # Go module dependencies
├── go.sum                 # Dependency checksums
├── main.go               # Application entry point
└── README.md             # This file
```

## 🔧 Development

### Running Tests
```bash
go test ./...
```

### Building for Production
```bash
go build -o weathering-with-go main.go
```

### Docker Support
```bash
# Build image
docker build -t weathering-with-go .

# Run container
docker run -p 8080:8080 -e OPENWEATHERMAP_API_KEY=your_key weathering-with-go
```

## 📊 API Limits

- **OpenWeatherMap Free Tier**: 1,000 calls/day, 60 calls/minute
- **Forecast**: Up to 5 days (OpenWeatherMap limitation)
- **Rate Limiting**: Built-in protection to prevent API abuse

## 📄 License

This project is licensed under the MIT License - see the LICENSE file for details.

## 🙏 Acknowledgments

- [OpenWeatherMap](https://openweathermap.org/) for providing the weather data API
- [Gin Framework](https://gin-gonic.com/) for the excellent HTTP web framework
- [Go Community](https://golang.org/) for the amazing programming language
