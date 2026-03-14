# Webmail Engine - Web Test Interface

A single-page web application for testing the Webmail Engine APIs.

## Quick Start

### 1. Start the Server

```bash
cd webmail_engine

# Set required environment variables
export ENCRYPTION_KEY="your-32-byte-encryption-key!!"

# Run the server
go run cmd/main.go
```

### 2. Open the Web Interface

Navigate to: **http://localhost:8080**

## Features

### Accounts Tab
- **Add Account**: Create new email accounts with IMAP/SMTP settings
- **Account List**: View all connected accounts with status
- **Delete Account**: Remove accounts
- **Idempotent Creation**: Submitting the same account twice updates instead of failing

### Messages Tab
- **Account Selector**: Choose which account to view messages from
- **Search**: Search messages by keyword
- **Message List**: View messages with sender, subject, and preview
- **Pagination**: Browse through message pages

### Compose Tab
- **From Account**: Select which account to send from
- **Recipient**: Enter recipient email address
- **Subject & Body**: Compose your message
- **Send**: Send email via SMTP

### Health Tab
- **Overall Status**: System health status and score
- **Component Status**: Health of individual components
- **Account Count**: Number of connected accounts

### Settings Tab
- **API URL**: Configure the backend API endpoint
- **Persistent**: Settings saved to browser localStorage

## Testing Scenarios

### 1. Create Account (First Time)
```
Email: test@gmail.com
Password: your-app-password
IMAP Host: imap.gmail.com
IMAP Port: 993
IMAP Encryption: SSL
SMTP Host: smtp.gmail.com
SMTP Port: 587
SMTP Encryption: STARTTLS
```
**Expected**: Account created, status "active"

### 2. Create Same Account Again (Idempotent)
Submit the exact same form again.

**Expected**: Returns existing account, status "already_running"

### 3. Update Account Password
Change only the password field and submit.

**Expected**: Account updated, status "reconfigured", old connections closed

### 4. Invalid Credentials
Enter wrong password.

**Expected**: Error toast with "SERVICE_UNAVAILABLE" or "AUTH_ERROR"

### 5. Test Without Redis
Stop Redis and restart server.

**Expected**: 
- Warning logged: "Redis not available, using in-memory cache"
- Web interface still works
- No panics on account creation

### 6. Connection Timeout
Use unreachable IMAP host (e.g., `imap.invalid.local`).

**Expected**: Error after 30 seconds with "TIMEOUT" error code

## API Response Format

All errors return structured JSON:

```json
{
  "error": {
    "code": "CONFLICT",
    "message": "Account already exists",
    "details": "Identifier: test@example.com"
  }
}
```

### Error Codes

| Code | HTTP Status | Meaning |
|------|-------------|---------|
| `CONFLICT` | 409 | Resource already exists |
| `TIMEOUT` | 504 | Operation timed out |
| `SERVICE_UNAVAILABLE` | 503 | External service down |
| `VALIDATION_ERROR` | 400 | Invalid input |
| `AUTH_ERROR` | 401 | Authentication failed |
| `RATE_LIMITED` | 429 | Too many requests |
| `NOT_FOUND` | 404 | Resource not found |
| `INTERNAL_ERROR` | 500 | Server error |

## Troubleshooting

### "Failed to connect to API"
- Check server is running on port 8080
- Verify API URL in top-right corner
- Check browser console for errors

### Account creation hangs
- Server logs show connection progress
- Timeout occurs after 30 seconds
- Check IMAP/SMTP credentials

### Toast notifications not showing
- Check browser console for JavaScript errors
- Ensure modern browser (Chrome, Firefox, Safari)

## Technical Details

- **No Build Tools**: Pure HTML/CSS/JS, no npm/webpack required
- **Vanilla JavaScript**: No frameworks (React, Vue, etc.)
- **Responsive Design**: Works on desktop and mobile
- **LocalStorage**: Saves API URL between sessions
- **Fetch API**: All API calls use native fetch()

## File Structure

```
web/
└── index.html    # Complete single-page app (HTML + CSS + JS)
```

The entire application is contained in a single 800-line HTML file for simplicity and ease of testing.
