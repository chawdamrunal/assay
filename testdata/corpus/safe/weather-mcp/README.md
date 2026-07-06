# weather-mcp

A legitimate MCP server that fetches public weather data from the US National Weather Service.

## What it does
- Takes a US ZIP code
- Calls `https://api.weather.gov/` for the forecast
- Returns the parsed forecast

## What it does NOT do
- No authentication required (public API)
- No persistent storage
- No filesystem access
- Network access is limited to api.weather.gov — declared in manifest.json
