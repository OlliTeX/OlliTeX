from flask import Flask, request, redirect, jsonify, render_template_string, session
import jwt
import secrets
import time
from urllib.parse import urlencode, parse_qs, urlparse
from datetime import datetime, timedelta

app = Flask(__name__)
app.secret_key = secrets.token_hex(32)

# Configuration
# Use HTTPS for issuer since that's what the browser sees
ISSUER = "https://overleaf.local/sso/realms/master"
CLIENT_ID = "overleaf_test"
CLIENT_SECRET = "SOMEPASSWORD"
VALID_REDIRECT_URI = "https://overleaf.local/oidc/login/callback"

# In-memory storage for codes and tokens
auth_codes = {}
users = {
    "test2@example.com": {
        "id": "user123",
        "email": "test2@example.com",
        "given_name": "Test",
        "family_name": "User",
        "password": "password"
    },
    "admin@example.com": {
        "id": "admin456",
        "email": "admin@example.com",
        "given_name": "Admin",
        "family_name": "User",
        "password": "admin",
        "is_admin": "true"
    }
}

# JWT signing key (in production, use proper key management)
JWT_SECRET = "your-secret-key-change-in-production"

LOGIN_PAGE = """
<!DOCTYPE html>
<html>
<head>
    <title>SSO Login</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            min-height: 100vh;
            margin: 0;
            background: #f5f5f5;
        }
        .login-box {
            background: white;
            padding: 2rem;
            border-radius: 8px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            width: 100%;
            max-width: 400px;
        }
        h2 {
            margin-top: 0;
            color: #333;
        }
        .form-group {
            margin-bottom: 1rem;
        }
        label {
            display: block;
            margin-bottom: 0.5rem;
            color: #666;
        }
        input {
            width: 100%;
            padding: 0.5rem;
            border: 1px solid #ddd;
            border-radius: 4px;
            box-sizing: border-box;
        }
        button {
            width: 100%;
            padding: 0.75rem;
            background: #007bff;
            color: white;
            border: none;
            border-radius: 4px;
            cursor: pointer;
            font-size: 1rem;
        }
        button:hover {
            background: #0056b3;
        }
        .error {
            color: #dc3545;
            margin-bottom: 1rem;
            padding: 0.75rem;
            background: #f8d7da;
            border: 1px solid #f5c6cb;
            border-radius: 4px;
        }
        .hint {
            margin-top: 1rem;
            padding: 1rem;
            background: #f8f9fa;
            border-radius: 4px;
            font-size: 0.875rem;
            color: #666;
        }
    </style>
</head>
<body>
    <div class="login-box">
        <h2>Benchmark SSO Login</h2>
        {% if error %}
        <div class="error">{{ error }}</div>
        {% endif %}
        <form method="POST">
            <div class="form-group">
                <label for="email">Email:</label>
                <input type="email" id="email" name="email" required>
            </div>
            <div class="form-group">
                <label for="password">Password:</label>
                <input type="password" id="password" name="password" required>
            </div>
            <button type="submit">Login</button>
        </form>
        <div class="hint">
            <strong>Test Accounts:</strong><br>
            • test2@example.com / password<br>
            • admin@example.com / admin
        </div>
    </div>
</body>
</html>
"""

ERROR_PAGE = """
<!DOCTYPE html>
<html>
<head>
    <title>Error</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            min-height: 100vh;
            margin: 0;
            background: #f5f5f5;
        }
        .error-box {
            background: white;
            padding: 2rem;
            border-radius: 8px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            max-width: 500px;
        }
        h2 {
            color: #dc3545;
            margin-top: 0;
        }
        .error-details {
            background: #f8f9fa;
            padding: 1rem;
            border-radius: 4px;
            margin-top: 1rem;
        }
        .error-code {
            font-family: monospace;
            color: #666;
        }
    </style>
</head>
<body>
    <div class="error-box">
        <h2>Authentication Error</h2>
        <p><strong>Error:</strong> {{ error }}</p>
        <p>{{ error_description }}</p>
        {% if error_uri %}
        <div class="error-details">
            <p class="error-code">Error Code: {{ error }}</p>
            <p><a href="{{ error_uri }}">More information</a></p>
        </div>
        {% endif %}
    </div>
</body>
</html>
"""

def create_jwt_token(user_data, token_type="access"):
    """Create a JWT token for the user"""
    now = int(time.time())
    
    if token_type == "id":
        # ID token with all user claims
        # Passport-openidconnect builds the profile from ID token claims
        payload = {
            "iss": ISSUER,
            "sub": user_data["id"],
            "aud": CLIENT_ID,
            "exp": now + 3600,
            "iat": now,
            "email": user_data["email"],
            "email_verified": True,
            "given_name": user_data.get("given_name", ""),
            "family_name": user_data.get("family_name", ""),
            "name": f"{user_data.get('given_name', '')} {user_data.get('family_name', '')}".strip(),
        }
        
        # Include is_admin in ID token so it's available in the profile
        if "is_admin" in user_data:
            payload["is_admin"] = user_data["is_admin"]
            
    else:
        # Access token has full claims for UserInfo endpoint
        payload = {
            "iss": ISSUER,
            "sub": user_data["id"],
            "aud": CLIENT_ID,
            "exp": now + 3600,
            "iat": now,
            "email": user_data["email"],
            "given_name": user_data.get("given_name", ""),
            "family_name": user_data.get("family_name", ""),
            "name": f"{user_data.get('given_name', '')} {user_data.get('family_name', '')}".strip(),
        }
        
        if "is_admin" in user_data:
            payload["is_admin"] = user_data["is_admin"]
    
    return jwt.encode(payload, JWT_SECRET, algorithm="HS256")

def redirect_with_error(redirect_uri, error, error_description, state=None):
    """Redirect back to client with error parameters"""
    params = {
        "error": error,
        "error_description": error_description
    }
    if state:
        params["state"] = state
    
    return redirect(f"{redirect_uri}?{urlencode(params)}")

@app.route("/")
@app.route("/health")
def health():
    """Health check endpoint"""
    return jsonify({
        "status": "ok",
        "service": "oidc-emulator",
        "version": "1.0"
    })

@app.route("/sso/realms/master/.well-known/openid-configuration")
def openid_configuration():
    """OIDC Discovery endpoint"""
    return jsonify({
        "issuer": ISSUER,
        "authorization_endpoint": f"{ISSUER}/protocol/openid-connect/auth",
        "token_endpoint": f"{ISSUER}/protocol/openid-connect/token",
        "userinfo_endpoint": f"{ISSUER}/protocol/openid-connect/userinfo",
        "end_session_endpoint": f"{ISSUER}/protocol/openid-connect/logout",
        "jwks_uri": f"{ISSUER}/protocol/openid-connect/certs",
        "response_types_supported": ["code"],
        "subject_types_supported": ["public"],
        "id_token_signing_alg_values_supported": ["HS256"],
        "scopes_supported": ["openid", "profile", "email"],
        "claims_supported": ["sub", "email", "name", "given_name", "family_name"],
        "grant_types_supported": ["authorization_code"]
    })

@app.route("/sso/realms/master/protocol/openid-connect/auth")
def authorize():
    """Authorization endpoint - shows login page"""
    client_id = request.args.get("client_id")
    redirect_uri = request.args.get("redirect_uri")
    response_type = request.args.get("response_type")
    state = request.args.get("state")
    scope = request.args.get("scope", "openid profile email")
    
    # Validate required parameters
    if not client_id:
        return render_template_string(ERROR_PAGE, 
            error="invalid_request",
            error_description="Missing required parameter: client_id",
            error_uri=None), 400
    
    if not redirect_uri:
        return render_template_string(ERROR_PAGE,
            error="invalid_request",
            error_description="Missing required parameter: redirect_uri",
            error_uri=None), 400
    
    if not response_type:
        return render_template_string(ERROR_PAGE,
            error="invalid_request",
            error_description="Missing required parameter: response_type",
            error_uri=None), 400
    
    # Validate client_id
    if client_id != CLIENT_ID:
        return render_template_string(ERROR_PAGE,
            error="unauthorized_client",
            error_description=f"Unknown client: {client_id}",
            error_uri=None), 401
    
    # Validate redirect_uri
    if redirect_uri != VALID_REDIRECT_URI:
        return render_template_string(ERROR_PAGE,
            error="invalid_request",
            error_description=f"Invalid redirect_uri. Expected: {VALID_REDIRECT_URI}",
            error_uri=None), 400
    
    # Validate response_type
    if response_type != "code":
        return redirect_with_error(
            redirect_uri,
            "unsupported_response_type",
            f"Response type '{response_type}' is not supported. Only 'code' is supported.",
            state
        )
    
    # Validate scope contains 'openid'
    scopes = scope.split()
    if "openid" not in scopes:
        return redirect_with_error(
            redirect_uri,
            "invalid_scope",
            "The 'openid' scope is required for OIDC authentication",
            state
        )
    
    # Store auth request in session
    session["auth_request"] = {
        "client_id": client_id,
        "redirect_uri": redirect_uri,
        "state": state,
        "scope": scope,
        "response_type": response_type
    }
    
    return render_template_string(LOGIN_PAGE, error=None)

@app.route("/sso/realms/master/protocol/openid-connect/auth", methods=["POST"])
def authorize_post():
    """Handle login form submission"""
    email = request.form.get("email")
    password = request.form.get("password")
    
    auth_request = session.get("auth_request")
    if not auth_request:
        return render_template_string(ERROR_PAGE,
            error="invalid_request",
            error_description="Session expired. Please try again.",
            error_uri=None), 400
    
    # Validate credentials
    if not email or not password:
        return render_template_string(LOGIN_PAGE, 
            error="Email and password are required")
    
    user = users.get(email)
    if not user or user["password"] != password:
        return render_template_string(LOGIN_PAGE, 
            error="Invalid email or password")
    
    # Generate authorization code
    code = secrets.token_urlsafe(32)
    auth_codes[code] = {
        "user": user,
        "client_id": auth_request["client_id"],
        "redirect_uri": auth_request["redirect_uri"],
        "scope": auth_request["scope"],
        "expires": time.time() + 600,  # 10 minutes
        "used": False
    }
    
    # Redirect back to client
    params = {
        "code": code,
    }
    if auth_request["state"]:
        params["state"] = auth_request["state"]
    
    return redirect(f"{auth_request['redirect_uri']}?{urlencode(params)}")

@app.route("/sso/realms/master/protocol/openid-connect/token", methods=["POST"])
def token():
    """Token endpoint - exchange code for tokens"""
    grant_type = request.form.get("grant_type")
    code = request.form.get("code")
    redirect_uri = request.form.get("redirect_uri")
    
    # Check for client credentials in Authorization header or form
    auth_header = request.headers.get("Authorization")
    if auth_header and auth_header.startswith("Basic "):
        import base64
        try:
            credentials = base64.b64decode(auth_header[6:]).decode('utf-8')
            client_id, client_secret = credentials.split(':', 1)
        except:
            return jsonify({
                "error": "invalid_client",
                "error_description": "Invalid Authorization header format"
            }), 401
    else:
        client_id = request.form.get("client_id")
        client_secret = request.form.get("client_secret")
    
    # Validate required parameters
    if not grant_type:
        return jsonify({
            "error": "invalid_request",
            "error_description": "Missing required parameter: grant_type"
        }), 400
    
    if not code:
        return jsonify({
            "error": "invalid_request",
            "error_description": "Missing required parameter: code"
        }), 400
    
    if not redirect_uri:
        return jsonify({
            "error": "invalid_request",
            "error_description": "Missing required parameter: redirect_uri"
        }), 400
    
    # Validate client credentials
    if not client_id or not client_secret:
        return jsonify({
            "error": "invalid_client",
            "error_description": "Client authentication failed"
        }), 401
    
    if client_id != CLIENT_ID or client_secret != CLIENT_SECRET:
        return jsonify({
            "error": "invalid_client",
            "error_description": "Invalid client credentials"
        }), 401
    
    # Validate grant type
    if grant_type != "authorization_code":
        return jsonify({
            "error": "unsupported_grant_type",
            "error_description": f"Grant type '{grant_type}' is not supported"
        }), 400
    
    # Validate authorization code
    auth_code = auth_codes.get(code)
    if not auth_code:
        return jsonify({
            "error": "invalid_grant",
            "error_description": "Invalid or expired authorization code"
        }), 400
    
    # Check if code has expired
    if auth_code["expires"] < time.time():
        del auth_codes[code]
        return jsonify({
            "error": "invalid_grant",
            "error_description": "Authorization code has expired"
        }), 400
    
    # Check if code has already been used (replay attack prevention)
    if auth_code.get("used"):
        del auth_codes[code]
        return jsonify({
            "error": "invalid_grant",
            "error_description": "Authorization code has already been used"
        }), 400
    
    # Validate redirect_uri matches
    if auth_code["redirect_uri"] != redirect_uri:
        return jsonify({
            "error": "invalid_grant",
            "error_description": "Redirect URI does not match authorization request"
        }), 400
    
    # Validate client_id matches
    if auth_code["client_id"] != client_id:
        return jsonify({
            "error": "invalid_grant",
            "error_description": "Client ID does not match authorization request"
        }), 400
    
    # Mark code as used
    auth_code["used"] = True
    
    # Generate tokens
    user = auth_code["user"]
    access_token = create_jwt_token(user, "access")
    id_token = create_jwt_token(user, "id")
    
    # Debug logging
    print(f"DEBUG - Token endpoint - User: {user.get('email')}, has is_admin: {'is_admin' in user}")
    
    # Clean up used code (after a delay to prevent race conditions)
    # In production, you'd use a background task
    
    return jsonify({
        "access_token": access_token,
        "token_type": "Bearer",
        "expires_in": 3600,
        "id_token": id_token,
        "scope": auth_code["scope"]
    })

@app.route("/sso/realms/master/protocol/openid-connect/userinfo", methods=["GET", "POST"])
def userinfo():
    """UserInfo endpoint"""
    # Support both GET and POST methods
    auth_header = request.headers.get("Authorization")
    
    if not auth_header:
        return jsonify({
            "error": "invalid_request",
            "error_description": "Missing Authorization header"
        }), 401
    
    if not auth_header.startswith("Bearer "):
        return jsonify({
            "error": "invalid_request",
            "error_description": "Invalid Authorization header format. Expected 'Bearer <token>'"
        }), 401
    
    token = auth_header[7:]
    
    if not token:
        return jsonify({
            "error": "invalid_token",
            "error_description": "Access token is missing"
        }), 401
    
    try:
        payload = jwt.decode(token, JWT_SECRET, algorithms=["HS256"], audience=CLIENT_ID)
        
        userinfo_response = {
            "sub": payload["sub"],
            "email": payload["email"],
            "email_verified": True,
            "name": payload["name"],
            "given_name": payload.get("given_name", ""),
            "family_name": payload.get("family_name", "")
        }
        
        # Include is_admin if present in the token
        if "is_admin" in payload:
            userinfo_response["is_admin"] = payload["is_admin"]
        
        # Debug logging
        print(f"DEBUG - UserInfo Response: {userinfo_response}")
        
        return jsonify(userinfo_response)
    except jwt.ExpiredSignatureError:
        return jsonify({
            "error": "invalid_token",
            "error_description": "Access token has expired"
        }), 401
    except jwt.InvalidAudienceError:
        return jsonify({
            "error": "invalid_token",
            "error_description": "Access token audience does not match"
        }), 401
    except jwt.DecodeError:
        return jsonify({
            "error": "invalid_token",
            "error_description": "Access token is malformed"
        }), 401
    except jwt.InvalidTokenError as e:
        return jsonify({
            "error": "invalid_token",
            "error_description": f"Invalid access token: {str(e)}"
        }), 401

@app.route("/sso/realms/master/protocol/openid-connect/logout")
def logout():
    """Logout endpoint"""
    id_token_hint = request.args.get("id_token_hint")
    post_logout_redirect_uri = request.args.get("post_logout_redirect_uri")
    state = request.args.get("state")
    
    # Validate id_token_hint if provided
    if id_token_hint:
        try:
            jwt.decode(id_token_hint, JWT_SECRET, algorithms=["HS256"], audience=CLIENT_ID)
        except jwt.InvalidTokenError:
            return render_template_string(ERROR_PAGE,
                error="invalid_request",
                error_description="Invalid id_token_hint",
                error_uri=None), 400
    
    # Clear session
    session.clear()
    
    # Redirect back to application
    if post_logout_redirect_uri:
        params = {}
        if state:
            params["state"] = state
        
        redirect_url = post_logout_redirect_uri
        if params:
            redirect_url += f"?{urlencode(params)}"
        
        return redirect(redirect_url)
    
    return "Logged out successfully", 200

@app.route("/sso/realms/master/protocol/openid-connect/certs")
def jwks():
    """JWKS endpoint - returns public keys for token verification"""
    # For HS256 (symmetric), we don't expose the key
    # In production with RS256, you'd return the public key here
    return jsonify({
        "keys": []
    })

@app.errorhandler(404)
def not_found(e):
    return jsonify({
        "error": "not_found",
        "error_description": "The requested endpoint does not exist"
    }), 404

@app.errorhandler(405)
def method_not_allowed(e):
    return jsonify({
        "error": "invalid_request",
        "error_description": f"Method {request.method} is not allowed for this endpoint"
    }), 405

@app.errorhandler(500)
def internal_error(e):
    return jsonify({
        "error": "server_error",
        "error_description": "An internal server error occurred"
    }), 500

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080, debug=True)
