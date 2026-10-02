from flask import Flask, request, redirect, render_template_string, session, make_response
import secrets
import time
import os
from datetime import datetime, timedelta
from lxml import etree
import base64
import zlib
from urllib.parse import urlencode, parse_qs, urlparse, unquote
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa, padding
from cryptography import x509
from cryptography.x509.oid import NameOID
from cryptography.hazmat.backends import default_backend
import uuid

app = Flask(__name__)
app.secret_key = secrets.token_hex(32)

# Configuration - can be overridden with environment variables
IDP_ENTITY_ID = os.getenv("IDP_ENTITY_ID", "https://overleaf.local/saml/idp")
SP_ENTITY_ID = os.getenv("SP_ENTITY_ID", "MyOverleaf")
SP_ACS_URL = os.getenv("SP_ACS_URL", "https://overleaf.local/saml/login/callback")
SP_SLS_URL = os.getenv("SP_SLS_URL", "https://overleaf.local/saml/logout/callback")

# Certificate persistence directory
CERT_DIR = os.getenv("CERT_DIR", "/app/certs")

# Attribute name configuration
ATTR_EMAIL = os.getenv("ATTR_EMAIL", "email")
ATTR_GIVEN_NAME = os.getenv("ATTR_GIVEN_NAME", "givenName")
ATTR_SURNAME = os.getenv("ATTR_SURNAME", "lastName")
ATTR_MAIL = os.getenv("ATTR_MAIL", "mail")
ATTR_IS_ADMIN = os.getenv("ATTR_IS_ADMIN", "is_admin")

# Paths for persistent certificates
PRIVATE_KEY_PATH = os.path.join(CERT_DIR, "saml_private_key.pem")
CERT_PATH = os.path.join(CERT_DIR, "saml_certificate.pem")

def ensure_cert_dir():
    """Ensure certificate directory exists"""
    os.makedirs(CERT_DIR, exist_ok=True)

def load_or_generate_keypair():
    """Load existing keypair or generate a new one"""
    ensure_cert_dir()
    
    if os.path.exists(PRIVATE_KEY_PATH) and os.path.exists(CERT_PATH):
        # Load existing key and certificate
        print("Loading existing certificate and private key...")
        with open(PRIVATE_KEY_PATH, 'rb') as f:
            private_key = serialization.load_pem_private_key(
                f.read(),
                password=None,
                backend=default_backend()
            )
        with open(CERT_PATH, 'rb') as f:
            cert = x509.load_pem_x509_certificate(f.read(), default_backend())
        print("✓ Loaded existing certificates")
        return private_key, cert
    else:
        # Generate new key pair
        print("Generating new certificate and private key...")
        private_key = rsa.generate_private_key(
            public_exponent=65537,
            key_size=2048,
        )
        
        # Generate self-signed certificate
        subject = issuer = x509.Name([
            x509.NameAttribute(NameOID.COUNTRY_NAME, u"US"),
            x509.NameAttribute(NameOID.STATE_OR_PROVINCE_NAME, u"Test"),
            x509.NameAttribute(NameOID.LOCALITY_NAME, u"Test"),
            x509.NameAttribute(NameOID.ORGANIZATION_NAME, u"SAML Test IdP"),
            x509.NameAttribute(NameOID.COMMON_NAME, u"overleaf.local"),
        ])
        
        cert = x509.CertificateBuilder().subject_name(
            subject
        ).issuer_name(
            issuer
        ).public_key(
            private_key.public_key()
        ).serial_number(
            x509.random_serial_number()
        ).not_valid_before(
            datetime.utcnow()
        ).not_valid_after(
            datetime.utcnow() + timedelta(days=3650)
        ).sign(private_key, hashes.SHA256())
        
        # Save to files
        with open(PRIVATE_KEY_PATH, 'wb') as f:
            f.write(private_key.private_bytes(
                encoding=serialization.Encoding.PEM,
                format=serialization.PrivateFormat.PKCS8,
                encryption_algorithm=serialization.NoEncryption()
            ))
        with open(CERT_PATH, 'wb') as f:
            f.write(cert.public_bytes(serialization.Encoding.PEM))
        
        print("✓ Generated and saved new certificates")
        return private_key, cert

# Load or generate keypair
private_key, cert = load_or_generate_keypair()
public_key = private_key.public_key()

# In-memory storage
auth_requests = {}

# User database with configurable attributes
def create_user(email, given_name, surname, is_admin=False, password="password"):
    """Helper function to create user with configurable attribute names"""
    user = {
        ATTR_EMAIL: email,
        ATTR_GIVEN_NAME: given_name,
        ATTR_SURNAME: surname,
        ATTR_MAIL: email,
        "password": password
    }
    if is_admin:
        user[ATTR_IS_ADMIN] = "true"
    return user

users = {
    "test@example.com": create_user("test@example.com", "Test", "User"),
    "admin@example.com": create_user("admin@example.com", "Admin", "User", is_admin=True, password="admin"),
    "overleaf.admin@example.com": create_user("overleaf.admin@example.com", "Super", "Admin", is_admin=True, password="admin"),
}

LOGIN_PAGE = """
<!DOCTYPE html>
<html>
<head>
    <title>SAML IdP Login</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            min-height: 100vh;
            margin: 0;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
        }
        .login-box {
            background: white;
            padding: 2rem;
            border-radius: 8px;
            box-shadow: 0 10px 40px rgba(0,0,0,0.2);
            width: 100%;
            max-width: 400px;
        }
        h2 {
            margin-top: 0;
            color: #333;
            text-align: center;
        }
        .idp-name {
            text-align: center;
            color: #667eea;
            font-size: 0.9rem;
            margin-bottom: 1.5rem;
        }
        .form-group {
            margin-bottom: 1rem;
        }
        label {
            display: block;
            margin-bottom: 0.5rem;
            color: #666;
            font-weight: 500;
        }
        input[type="email"],
        input[type="password"] {
            width: 100%;
            padding: 0.75rem;
            border: 2px solid #e0e0e0;
            border-radius: 4px;
            box-sizing: border-box;
            font-size: 1rem;
            transition: border-color 0.3s;
        }
        input[type="email"]:focus,
        input[type="password"]:focus {
            outline: none;
            border-color: #667eea;
        }
        button {
            width: 100%;
            padding: 0.75rem;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            border: none;
            border-radius: 4px;
            cursor: pointer;
            font-size: 1rem;
            font-weight: 600;
            transition: transform 0.2s;
        }
        button:hover {
            transform: translateY(-2px);
        }
        button:active {
            transform: translateY(0);
        }
        .error {
            color: #dc3545;
            margin-bottom: 1rem;
            padding: 0.75rem;
            background: #f8d7da;
            border: 1px solid #f5c6cb;
            border-radius: 4px;
            font-size: 0.9rem;
        }
        .hint {
            margin-top: 1.5rem;
            padding: 1rem;
            background: #f8f9fa;
            border-radius: 4px;
            font-size: 0.875rem;
            color: #666;
            border-left: 4px solid #667eea;
        }
        .hint strong {
            display: block;
            margin-bottom: 0.5rem;
            color: #333;
        }
        .hint-line {
            padding: 0.25rem 0;
        }
    </style>
</head>
<body>
    <div class="login-box">
        <h2>SAML Authentication</h2>
        <div class="idp-name">Test Identity Provider</div>
        {% if error %}
        <div class="error">{{ error }}</div>
        {% endif %}
        <form method="POST">
            <div class="form-group">
                <label for="email">Email:</label>
                <input type="email" id="email" name="email" required autofocus>
            </div>
            <div class="form-group">
                <label for="password">Password:</label>
                <input type="password" id="password" name="password" required>
            </div>
            <input type="hidden" name="SAMLRequest" value="{{ saml_request }}">
            <input type="hidden" name="RelayState" value="{{ relay_state }}">
            <button type="submit">Sign In</button>
        </form>
        <div class="hint">
            <strong>📋 Test Accounts</strong>
            <div class="hint-line">👤 test@example.com / password</div>
            <div class="hint-line">👨‍💼 admin@example.com / admin</div>
            <div class="hint-line">⭐ overleaf.admin@example.com / admin</div>
        </div>
    </div>
</body>
</html>
"""

def decode_saml_request(saml_request):
    """Decode and inflate SAML request"""
    decoded = base64.b64decode(saml_request)
    try:
        inflated = zlib.decompress(decoded, -15)
        return etree.fromstring(inflated)
    except zlib.error:
        return etree.fromstring(decoded)

def sign_xml(xml_string):
    """Sign XML assertion using XMLDSig (simplified)"""
    # For a complete implementation, use signxml library
    # This is a simplified version for testing
    return xml_string

def create_saml_response(user, request_id, acs_url, session_index):
    """Create SAML Response XML with signature"""
    now = datetime.utcnow()
    issue_instant = now.strftime('%Y-%m-%dT%H:%M:%SZ')
    not_before = now.strftime('%Y-%m-%dT%H:%M:%SZ')
    not_on_or_after = (now + timedelta(hours=1)).strftime('%Y-%m-%dT%H:%M:%SZ')
    
    assertion_id = f"_{uuid.uuid4().hex}" # SAML IDs usually start with an underscore
    response_id = f"_{uuid.uuid4().hex}"
    
    email = user.get(ATTR_EMAIL) or user.get(ATTR_MAIL)
    
    attributes = []
    for key, value in user.items():
        if key != 'password':
            attributes.append(f'''
                <saml:Attribute Name="{key}" NameFormat="urn:oasis:names:tc:SAML:2.0:attrname-format:basic">
                    <saml:AttributeValue xmlns:xs="http://www.w3.org/2001/XMLSchema" 
                                         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" 
                                         xsi:type="xs:string">{value}</saml:AttributeValue>
                </saml:Attribute>''')
    
    attributes_xml = '\n'.join(attributes)
    
    # Template using precise namespaces
    saml_response = f'''<?xml version="1.0" encoding="UTF-8"?>
<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"
                xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"
                ID="{response_id}"
                Version="2.0"
                IssueInstant="{issue_instant}"
                Destination="{acs_url}"
                InResponseTo="{request_id}">
    <saml:Issuer>{IDP_ENTITY_ID}</saml:Issuer>
    <samlp:Status>
        <samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/>
    </samlp:Status>
    <saml:Assertion ID="{assertion_id}"
                    Version="2.0"
                    IssueInstant="{issue_instant}"
                    xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
        <saml:Issuer>{IDP_ENTITY_ID}</saml:Issuer>
        <saml:Subject>
            <saml:NameID Format="urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress">{email}</saml:NameID>
            <saml:SubjectConfirmation Method="urn:oasis:names:tc:SAML:2.0:cm:bearer">
                <saml:SubjectConfirmationData NotOnOrAfter="{not_on_or_after}"
                                              Recipient="{acs_url}"
                                              InResponseTo="{request_id}"/>
            </saml:SubjectConfirmation>
        </saml:Subject>
        <saml:Conditions NotBefore="{not_before}" NotOnOrAfter="{not_on_or_after}">
            <saml:AudienceRestriction>
                <saml:Audience>{SP_ENTITY_ID}</saml:Audience>
            </saml:AudienceRestriction>
        </saml:Conditions>
        <saml:AuthnStatement AuthnInstant="{issue_instant}" SessionIndex="{session_index}">
            <saml:AuthnContext>
                <saml:AuthnContextClassRef>urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport</saml:AuthnContextClassRef>
            </saml:AuthnContext>
        </saml:AuthnStatement>
        <saml:AttributeStatement>
            {attributes_xml}
        </saml:AttributeStatement>
    </saml:Assertion>
</samlp:Response>'''
    
    try:
        from signxml import XMLSigner, methods
        
        # Use PEM-encoded key/cert for signxml
        key_pem = private_key.private_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PrivateFormat.PKCS8,
            encryption_algorithm=serialization.NoEncryption()
        )
        cert_pem = cert.public_bytes(serialization.Encoding.PEM)
        
        # Create signer with explicit algorithms and enveloped method
        signer = XMLSigner(method=methods.enveloped,
                           signature_algorithm="rsa-sha256",
                           digest_algorithm="sha256",
                           c14n_algorithm="http://www.w3.org/2001/10/xml-exc-c14n#")
        
        root = etree.fromstring(saml_response.encode('utf-8'))
        # Locate the Assertion element specifically
        assertion = root.find('.//{urn:oasis:names:tc:SAML:2.0:assertion}Assertion')
        
        # Sign the assertion element (reference to its ID)
        signed_assertion = signer.sign(
            assertion,
            key=key_pem,
            cert=cert_pem,
            reference_uri=f"#{assertion_id}",
        )
        # Replace old assertion with signed assertion
        parent = assertion.getparent()
        parent.replace(assertion, signed_assertion)
        
        # Also sign the Response element (some SPs validate the Response signature)
        # Note: signxml will insert a Signature element into the Response
        signed_response = signer.sign(
            root,
            key=key_pem,
            cert=cert_pem,
            reference_uri=f"#{response_id}",
        )
        
        # Debug: verify Signature elements are present
        sigs = signed_response.findall('.//{http://www.w3.org/2000/09/xmldsig#}Signature')
        print(f"DEBUG - Signature count after signing: {len(sigs)}")
        
        return etree.tostring(signed_response, encoding='unicode')
        
    except Exception as e:
        print(f"ERROR creating SAML response: {e}")
        import traceback
        traceback.print_exc()
        return saml_response

@app.route("/")
@app.route("/health")
def health():
    """Health check endpoint"""
    return {
        "status": "ok",
        "service": "saml-emulator",
        "certificate_persistent": os.path.exists(CERT_PATH),
        "config": {
            "idp_entity_id": IDP_ENTITY_ID,
            "sp_entity_id": SP_ENTITY_ID,
            "attributes": {
                "email": ATTR_EMAIL,
                "givenName": ATTR_GIVEN_NAME,
                "surname": ATTR_SURNAME,
                "mail": ATTR_MAIL,
                "isAdmin": ATTR_IS_ADMIN
            }
        }
    }

@app.route("/saml/idp/metadata")
def metadata():
    """IdP Metadata endpoint"""
    cert_pem = cert.public_bytes(serialization.Encoding.PEM).decode('utf-8')
    cert_data = cert_pem.replace('-----BEGIN CERTIFICATE-----\n', '').replace('-----END CERTIFICATE-----\n', '').replace('\n', '')
    
    sso_url = f"{IDP_ENTITY_ID.rsplit('/idp', 1)[0]}/idp/SSOService"
    slo_url = f"{IDP_ENTITY_ID.rsplit('/idp', 1)[0]}/idp/SingleLogoutService"
    
    metadata_xml = f'''<?xml version="1.0" encoding="UTF-8"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata"
                  entityID="{IDP_ENTITY_ID}">
    <IDPSSODescriptor WantAuthnRequestsSigned="false"
                      protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
        <KeyDescriptor use="signing">
            <ds:KeyInfo xmlns:ds="http://www.w3.org/2000/09/xmldsig#">
                <ds:X509Data>
                    <ds:X509Certificate>{cert_data}</ds:X509Certificate>
                </ds:X509Data>
            </ds:KeyInfo>
        </KeyDescriptor>
        <SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"
                           Location="{slo_url}"/>
        <SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
                           Location="{slo_url}"/>
        <NameIDFormat>urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress</NameIDFormat>
        <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"
                            Location="{sso_url}"/>
        <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
                            Location="{sso_url}"/>
    </IDPSSODescriptor>
</EntityDescriptor>'''
    
    response = make_response(metadata_xml)
    response.headers['Content-Type'] = 'application/xml'
    return response

@app.route("/saml/idp/SSOService", methods=['GET', 'POST'])
def sso_service():
    """Single Sign-On Service"""
    if request.method == 'GET':
        saml_request = request.args.get('SAMLRequest')
        relay_state = request.args.get('RelayState', '')
    else:
        saml_request = request.form.get('SAMLRequest')
        relay_state = request.form.get('RelayState', '')
    
    if not saml_request:
        return "Missing SAMLRequest parameter", 400
    
    email = request.form.get('email')
    password = request.form.get('password')
    
    if email and password:
        return process_login(email, password, saml_request, relay_state)
    
    try:
        authn_request = decode_saml_request(saml_request)
        request_id = authn_request.get('ID')
        acs_url = authn_request.get('AssertionConsumerServiceURL', SP_ACS_URL)
        
        auth_requests[request_id] = {
            'acs_url': acs_url,
            'relay_state': relay_state,
            'timestamp': time.time()
        }
        
        print(f"DEBUG - Received AuthnRequest: ID={request_id}, ACS={acs_url}")
        
        return render_template_string(
            LOGIN_PAGE,
            error=None,
            saml_request=saml_request,
            relay_state=relay_state
        )
    except Exception as e:
        print(f"Error processing SAML request: {e}")
        import traceback
        traceback.print_exc()
        return f"Error processing SAML request: {e}", 400

def process_login(email, password, saml_request_b64, relay_state):
    """Process login form submission"""
    user = users.get(email)
    if not user or user['password'] != password:
        return render_template_string(
            LOGIN_PAGE,
            error="Invalid email or password. Please try again.",
            saml_request=saml_request_b64,
            relay_state=relay_state
        )
    
    try:
        authn_request = decode_saml_request(saml_request_b64)
        request_id = authn_request.get('ID')
        acs_url = authn_request.get('AssertionConsumerServiceURL', SP_ACS_URL)
        
        session_index = f"__{uuid.uuid4().hex}"
        
        saml_response_xml = create_saml_response(user, request_id, acs_url, session_index)
        saml_response_b64 = base64.b64encode(saml_response_xml.encode('utf-8')).decode('utf-8')
        
        print(f"✓ Authentication successful: {email}")
        print(f"  SessionIndex: {session_index}")
        print(f"  Attributes sent: {[k for k in user.keys() if k != 'password']}")
        
        form_html = f'''
<!DOCTYPE html>
<html>
<head>
    <title>SAML Response</title>
    <style>
        body {{
            font-family: Arial, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            min-height: 100vh;
            margin: 0;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
        }}
        .message {{
            background: white;
            padding: 2rem;
            border-radius: 8px;
            box-shadow: 0 10px 40px rgba(0,0,0,0.2);
            text-align: center;
        }}
        .spinner {{
            border: 4px solid #f3f3f3;
            border-top: 4px solid #667eea;
            border-radius: 50%;
            width: 40px;
            height: 40px;
            animation: spin 1s linear infinite;
            margin: 0 auto 1rem;
        }}
        @keyframes spin {{
            0% {{ transform: rotate(0deg); }}
            100% {{ transform: rotate(360deg); }}
        }}
    </style>
</head>
<body onload="document.forms[0].submit()">
    <div class="message">
        <div class="spinner"></div>
        <p>Redirecting to Overleaf...</p>
    </div>
    <form method="POST" action="{acs_url}">
        <input type="hidden" name="SAMLResponse" value="{saml_response_b64}"/>
        <input type="hidden" name="RelayState" value="{relay_state}"/>
        <noscript>
            <button type="submit">Continue</button>
        </noscript>
    </form>
</body>
</html>'''
        
        return form_html
        
    except Exception as e:
        print(f"✗ Error creating SAML response: {e}")
        import traceback
        traceback.print_exc()
        return f"Error creating SAML response: {e}", 500

@app.route("/saml/idp/SingleLogoutService", methods=['GET', 'POST'])
def single_logout_service():
    """Single Logout Service"""
    session.clear()
    
    if request.method == 'GET':
        relay_state = request.args.get('RelayState', '')
    else:
        relay_state = request.form.get('RelayState', '')
    
    print("✓ User logged out")
    
    if relay_state:
        return redirect(relay_state)
    
    return "Logged out successfully", 200

@app.route("/saml/idp/certs/idp_cert.pem")
def get_certificate():
    """Download IdP certificate"""
    cert_pem = cert.public_bytes(serialization.Encoding.PEM)
    response = make_response(cert_pem)
    response.headers['Content-Type'] = 'application/x-pem-file'
    response.headers['Content-Disposition'] = 'attachment; filename=idp_cert.pem'
    return response

if __name__ == "__main__":
    cert_pem = cert.public_bytes(serialization.Encoding.PEM).decode('utf-8')
    print("=" * 80)
    print("SAML IdP Emulator - Configuration")
    print("=" * 80)
    print(f"IdP Entity ID: {IDP_ENTITY_ID}")
    print(f"SP Entity ID:  {SP_ENTITY_ID}")
    print(f"ACS URL:       {SP_ACS_URL}")
    print(f"SLS URL:       {SP_SLS_URL}")
    print(f"Cert persisted: {os.path.exists(CERT_PATH)}")
    print("=" * 80)
    print("Attribute Mappings:")
    print(f"  Email:      {ATTR_EMAIL}")
    print(f"  First Name: {ATTR_GIVEN_NAME}")
    print(f"  Last Name:  {ATTR_SURNAME}")
    print(f"  Mail:       {ATTR_MAIL}")
    print(f"  Is Admin:   {ATTR_IS_ADMIN}")
    print("=" * 80)
    print("Certificate (save as idp_cert.pem):")
    print("=" * 80)
    print(cert_pem)
    print("=" * 80)
    print("\nStarting server on http://0.0.0.0:8081")
    print("Health check: http://localhost:8081/health")
    print("Metadata:     http://localhost:8081/saml/idp/metadata")
    print("Certificate:  http://localhost:8081/saml/idp/certs/idp_cert.pem")
    print("=" * 80)
    
    app.run(host="0.0.0.0", port=8081, debug=True)