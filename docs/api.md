# URL Shortener API Documentation

## Shorten URL

Create a shortened URL from a long URL.

**Endpoint:** `POST /api/v1/shorten`

**Request Body:**
```json
{
  "url": "https://example.com/very/long/url",  // Required
  "title": "My Link",                          // Optional
  "slug": "custom-slug",                       // Optional
  "expiry_in_secs": 3600                      // Optional, in seconds
}
```

**Response:**
```json
{
  "status": "success",
  "data": {
    "short_code": "abc123"
  }
}
```

**Error Response:**
```json
{
  "status": "error",
  "message": "Error message here"
}
```

## Get URLs

Retrieve a paginated list of shortened URLs.

**Endpoint:** `GET /api/v1/urls`

**Query Parameters:**
- `page`: Page number (default: 1)
- `per_page`: Items per page (default: 10)

**Response:**
```json
{
  "status": "success",
  "data": {
    "urls": [
      {
        "url": "https://example.com/long/url",
        "title": "My Link",
        "short_code": "abc123",
        "created_at": "2024-01-01T00:00:00Z",
        "expires_at": "2024-01-02T00:00:00Z"
      }
    ],
    "page": 1,
    "per_page": 10,
    "count": 1
  }
}
```

## Health Check

Check if the service is healthy.

**Endpoint:** `GET /api/v1/health`

**Response:**
```json
{
  "status": "success",
  "data": "healthy"
}
```
