# Generic Go Proxy for NocoDB

**A stable, secure API layer between your applications and NocoDB**

This proxy sits between your client applications (web, mobile, desktop) and NocoDB, providing authentication, authorization, and a clean API that adapts to your database schema automatically.

Instead of exposing NocoDB directly to clients—with all the security risks and complexity that entails—this proxy gives you a controlled, frontend-safe interface that just works.

## The Problem This Solves

If you've ever built a client application on top of NocoDB, you've likely run into these issues:

**🔓 Security Risks**
- Database credentials exposed in client code
- No way to enforce user-specific data access
- Anyone with the API token can access everything

**🔧 Fragile Integrations**
- Hardcoded table IDs scattered across your frontend
- Schema changes break your application
- Every client needs to implement auth and filtering logic separately

**📝 Developer Experience**
- Cryptic table IDs like `m7rl42lk4m0nq27` instead of readable names
- Repeated authentication code across multiple frontends
- No central place to manage access control

This proxy solves all of these problems by providing a single, stable layer that handles security, schema awareness, and access control for you.

---

## Why This Proxy Exists

NocoDB is a powerful database platform, but it's designed to be accessed from trusted environments. When you call NocoDB directly from client applications:

- **Your database token lives in client code** where anyone can extract it
- **There's no user authentication** — just a shared API token
- **Schema changes require code updates** across all your clients
- **Access control logic gets duplicated** in every application

This proxy absorbs that complexity **once, centrally**. It provides:

✅ **Token isolation** — Database credentials never leave the server  
✅ **User authentication** — JWT-based login with role support  
✅ **Automatic schema awareness** — Friendly table names that adapt to your database  
✅ **Centralized access control** — Row-level filtering applied consistently  
✅ **Stable API** — Clients use clean URLs that don't break when your schema evolves

You get the flexibility of NocoDB with the security and stability of a proper backend API.

---

## What This Proxy Enables

Think of this proxy as a **capability layer** for NocoDB. It transforms your database into a secure, multi-client platform:

### Schema-Driven Behavior
The proxy discovers your database structure at startup and adapts automatically. Add a new table in NocoDB, and clients can access it immediately using its friendly name—no code changes required.

### Frontend-Safe APIs
Clients use clean, readable URLs like `/proxy/products/records` instead of cryptic IDs. The proxy handles all the complexity of resolving names to internal identifiers.

### Centralized Access Control
Define authentication and authorization rules once. Every client—web, mobile, desktop, or API consumer—gets the same security guarantees automatically.

### Multi-Client Support
Build as many frontends as you need. They all share the same secure API, the same authentication system, and the same access control rules.

---

## Who This Is For

This proxy is designed for:

**Frontend Developers**  
You get a clean, predictable API without worrying about database internals or security tokens.

**Full-Stack Teams**  
You can iterate on your database schema without breaking client applications.

**Internal Tooling Teams**  
You can build admin dashboards, mobile apps, and automation scripts that all share the same secure backend.

**Startups & Indie Builders**  
You get enterprise-grade security and architecture without the complexity of building a custom backend.

**Anyone Building on NocoDB**  
If you're building more than one client application, or if you need user authentication and access control, this proxy saves you from reinventing the wheel.

---

## Example Use Cases

This proxy is domain-agnostic and works with any NocoDB schema. Here are some real-world examples:

**📊 Admin Dashboards**  
Build internal tools where different users see different data based on their role and permissions.

**🛍️ Product Catalogs**  
Create customer-facing applications that browse and search products without exposing your database.

**📝 CRM & Sales Tools**  
Manage contacts, accounts, and opportunities with proper user isolation and access control.

**💰 Quote & Invoice Systems**  
Generate quotes, track orders, and manage customer relationships with row-level security.

**📱 Mobile Applications**  
Build iOS or Android apps that securely access your NocoDB data through a stable API.

**🤖 AI & Automation Tools**  
Give AI agents or automation scripts structured access to your data with proper authentication.

**📦 Inventory Management**  
Track products, warehouses, and stock levels with multi-user access and audit trails.

The proxy doesn't care what your tables represent—it works with any schema you define in NocoDB.

---

## How It Works

Here's what happens when a client makes a request:

```
┌─────────────┐
│   Client    │  Sends: GET /proxy/products/records
│ Application │         Authorization: Bearer <jwt>
└──────┬──────┘
       │
       ▼
┌─────────────────────────────────────────────┐
│              Generic Proxy                  │
│                                             │
│  1. Validates JWT token                     │
│  2. Extracts user ID and role               │
│  3. Applies row-level filtering (if needed) │
│  4. Resolves "products" → table ID          │
│  5. Forwards to NocoDB with secure token    │
│                                             │
└──────┬──────────────────────────────────────┘
       │
       ▼
┌─────────────┐
│   NocoDB    │  Processes request with actual table ID
│  Database   │  Returns filtered data
└─────────────┘
```

**Step by step:**

1. **Client authenticates** using email/password and receives a JWT token
2. **Client makes requests** using friendly table names like `products` or `orders`
3. **Proxy validates** the JWT and extracts user information
4. **Proxy applies security** by filtering data to what the user is allowed to see
5. **Proxy resolves names** to NocoDB's internal table identifiers
6. **Proxy forwards** the request to NocoDB with the secure database token
7. **NocoDB returns data** which flows back to the client

The client never sees database credentials, internal table IDs, or implementation details. It just gets a clean, secure API.

---

## How to Use This Proxy

### Prerequisites

- **Go 1.21+** installed on your system
- **NocoDB instance** running (self-hosted or cloud)
- Your NocoDB **base ID** and **API token**

### Starting the Proxy

1. **Clone and configure**

```bash
# Clone the repository
git clone <your-repo-url>
cd proxy

# Copy environment template
cp .env.example .env
```

2. **Edit `.env` with your NocoDB details**

```env
PORT=8080
NOCODB_URL=http://localhost:8090/api/v3/data/project/
NOCODB_BASE_ID=your_base_id_here
NOCODB_TOKEN=your_nocodb_api_token
JWT_SECRET=your_secure_random_secret
```

3. **Start the proxy**

```bash
go mod download
go run main.go
```

You'll see output like:

```
[STARTUP] Initializing Generic Proxy Server...
[META] Mapped 'Products' -> 'm7rl42lk4m0nq27'
[META] Mapped 'Orders' -> 'n8sm53mc5j7wc39'
[META] Successfully loaded 5 table mappings from NocoDB
[STARTUP] Server listening on :8080
```

The proxy is now running and ready to accept requests.

### Authenticating and Getting a Token

Before accessing data, clients need to authenticate:

```bash
curl -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "password123"
  }'
```

**Response:**

```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user_id": "user-001",
  "role": "user"
}
```

Save this token—you'll include it in all subsequent requests.

### Accessing Data Using Friendly Names

Now you can access your NocoDB tables using readable names:

```bash
# List all products
curl http://localhost:8080/proxy/products/records \
  -H "Authorization: Bearer <your-token>"

# Get a specific order
curl http://localhost:8080/proxy/orders/records/17 \
  -H "Authorization: Bearer <your-token>"

# Create a new task
curl -X POST http://localhost:8080/proxy/tasks/records \
  -H "Authorization: Bearer <your-token>" \
  -H "Content-Type: application/json" \
  -d '{"title": "Review proposal", "status": "pending"}'

# Update a customer record
curl -X PATCH http://localhost:8080/proxy/customers/records/rec456 \
  -H "Authorization: Bearer <your-token>" \
  -H "Content-Type: application/json" \
  -d '{"status": "active"}'
```

The proxy automatically:
- Resolves `products`, `orders`, `tasks`, `customers` to their NocoDB table IDs
- Applies row-level filtering so users only see their own data
- Injects the secure NocoDB token
- Returns clean JSON responses

### Using the Proxy from a Frontend Application

Here's a simple example in JavaScript:

```javascript
// Login and store token
const login = async (email, password) => {
  const response = await fetch('http://localhost:8080/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password })
  });
  const { token } = await response.json();
  localStorage.setItem('token', token);
};

// Fetch data using friendly table names
const getProducts = async () => {
  const token = localStorage.getItem('token');
  const response = await fetch('http://localhost:8080/proxy/products/records', {
    headers: { 'Authorization': `Bearer ${token}` }
  });
  return response.json();
};

// Create a new record
const createOrder = async (orderData) => {
  const token = localStorage.getItem('token');
  const response = await fetch('http://localhost:8080/proxy/orders/records', {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(orderData)
  });
  return response.json();
};
```

Your frontend code stays clean and readable—no cryptic IDs, no database tokens, just simple API calls.

### Linking Related Records (Optional)

If your NocoDB tables have relationships (link fields), you can manage them through the proxy:

```bash
# Link products to an order
curl -X POST http://localhost:8080/proxy/orders/links/products/rec123 \
  -H "Authorization: Bearer <your-token>" \
  -H "Content-Type: application/json" \
  -d '[{"id": "prod1"}, {"id": "prod2"}]'

# Get linked records
curl http://localhost:8080/proxy/orders/links/products/rec123 \
  -H "Authorization: Bearer <your-token>"
```

The proxy handles link field resolution automatically.

---

## Schema Awareness (MetaCache)

One of the proxy's key features is **automatic schema awareness**. Instead of hardcoding table IDs, the proxy discovers your database structure at startup.

### How It Works

When the proxy starts:

1. **Fetches metadata** from NocoDB's schema API
2. **Builds a mapping** of table names to internal IDs
3. **Caches this mapping** in memory for fast lookups
4. **Refreshes automatically** every 10 minutes to stay in sync

### What This Means for You

**No Hardcoded IDs**  
Your client code uses friendly names like `products` or `customers`. The proxy handles the translation to NocoDB's internal identifiers.

**Automatic Adaptation**  
Add a new table in NocoDB, and clients can access it as soon as the MetaCache refreshes. Rename a table, and the proxy picks up the change on the next refresh.

**Consistent Experience**  
Whether you're accessing `products`, `orders`, or `inventory`, the API works the same way. The proxy abstracts away NocoDB's internal structure.

### Example

Your NocoDB might have a table called "Products" with internal ID `m7rl42lk4m0nq27`.

**Without the proxy**, clients would need to know:
```
GET /api/v3/data/pbf7tt48gxdl50h/m7rl42lk4m0nq27/records
```

**With the proxy**, clients just use:
```
GET /proxy/products/records
```

The proxy resolves `products` → `m7rl42lk4m0nq27` automatically, and your client code stays readable and maintainable.

---

## Configuration (proxy.yaml)

While the proxy automatically discovers your NocoDB schema, you can optionally use `proxy.yaml` to explicitly control which tables are accessible and what operations are allowed.

### What It Controls

**Table Access** — Define which tables clients can access using friendly logical keys.

**Operation Permissions** — Whitelist specific operations (read, create, update, delete, link) per table.

**Explicit Validation** — The proxy validates requests against your configuration before forwarding to NocoDB.

This complements the automatic schema discovery by adding an explicit security and access control layer.

### Example Configuration

```yaml
nocodb:
  base_id: "your_base_id_here"

tables:
  # Logical key used in URLs
  products:
    name: "Products"  # Actual NocoDB table name
    operations: [read]  # Read-only access
  
  orders:
    name: "Orders"
    operations: [read, create, update]  # No delete allowed
  
  inventory:
    name: "Inventory"
    operations: [read, create, update, delete, link]  # Full access
```

**How it works:**
- Clients use the logical key (`products`, `orders`) in API calls
- The proxy resolves the table name (`Products`, `Orders`) via MetaCache
- Operations are validated against the whitelist before execution
- Unauthorized operations return `403 Forbidden`

This gives you fine-grained control over what each table allows, independent of user roles.

---

## Security & Access Control

Security is built into every layer of this proxy. Your database credentials stay on the server, users authenticate with JWT tokens, and row-level filtering ensures users only see their own data. All access is logged for audit purposes.

**Token Isolation** — NocoDB credentials never leave the server. Clients use JWT tokens that the proxy validates.

**User Authentication** — JWT-based login with 24-hour token expiry. Tokens contain user ID and role information.

**Row-Level Filtering** — Non-admin users automatically see only their own records. Filtering happens at the proxy layer with no client-side bypass.

**Centralized Authorization** — Define access rules once. Every client gets the same security guarantees automatically.

**Audit Logging** — All requests are logged with user ID, table accessed, timestamp, and success/failure status.

---

## Design Principles

This proxy is built on a few core principles:

### Schema-Driven

The proxy adapts to your database schema automatically. It doesn't contain hardcoded table names or business logic. This makes it reusable across different applications and use cases.

### Domain-Agnostic

Whether you're building a CRM, an e-commerce platform, or an inventory system, the proxy works the same way. It doesn't know or care what your tables represent—it just provides secure, stable access.

### Secure by Default

Security isn't optional or added later. It's baked into the architecture:
- Credentials isolated on the server
- Authentication required for all data access
- Row-level filtering applied automatically
- Comprehensive audit logging

### Frontend-Safe

The API is designed for client applications:
- Clean, readable URLs
- Predictable request/response patterns
- No database internals exposed
- Stable interface that doesn't break with schema changes

### Reusable Across Applications

One proxy can serve multiple frontends:
- Web applications
- Mobile apps
- Desktop tools
- API consumers
- Automation scripts

They all share the same authentication, authorization, and data access layer.

---

## Project Structure & Configuration

### Directory Layout

```
proxy/
├── main.go                 # Server entry point
├── internal/
│   ├── auth/              # Authentication handlers
│   ├── config/            # Configuration loading
│   ├── middleware/        # Auth & authorization middleware
│   ├── proxy/             # Core proxy logic & MetaCache
│   └── utils/             # JWT utilities
├── .env.example           # Environment template
└── go.mod                 # Go dependencies
```

### Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `PORT` | Server port | No (default: 8080) |
| `NOCODB_URL` | NocoDB API base URL | Yes |
| `NOCODB_BASE_ID` | Your NocoDB base ID | Yes |
| `NOCODB_TOKEN` | NocoDB API token | Yes |
| `JWT_SECRET` | Secret for signing JWT tokens | Yes |

### Demo Users

For testing, the proxy includes two demo users:

| Email | Password | Role | Access |
|-------|----------|------|--------|
| `admin@example.com` | `admin123` | admin | All records |
| `user@example.com` | `user123` | user | Own records only |

In production, you'd integrate with your own user database or authentication provider.

---

## Open Source & Extensibility

This proxy is designed to be reusable and extensible.

### Use It As-Is

The proxy works out of the box for most use cases. Point it at your NocoDB instance, configure your environment variables, and start building.

### Extend It

The codebase is structured to make common extensions straightforward:

- **Add custom authentication** — Integrate with OAuth, SAML, or your existing auth system
- **Implement custom authorization** — Add role-based or attribute-based access control
- **Add caching** — Cache frequent queries for better performance
- **Add rate limiting** — Protect your NocoDB instance from abuse
- **Add webhooks** — Trigger actions when data changes

### Contribute

Contributions are welcome! Whether it's:
- Bug fixes
- New features
- Documentation improvements
- Example integrations

Feel free to open issues or submit pull requests.

### Roadmap

Potential future enhancements:

- [ ] GraphQL API support
- [ ] WebSocket support for real-time updates
- [ ] Response caching layer
- [ ] Rate limiting and request throttling
- [ ] Multi-tenant support
- [ ] Admin dashboard UI
- [ ] Webhook support for data change notifications

---

## Getting Help

If you run into issues or have questions:

1. **Check the logs** — The proxy logs detailed information about every request
2. **Review the documentation** — Most common questions are covered here
3. **Open an issue** — Describe what you're trying to do and what's not working

This proxy is built to be straightforward and reliable. If something's confusing or not working as expected, that's a bug in the documentation or the code—let us know so we can fix it.

---

**Built with ❤️ for the NocoDB community**

If this proxy helps you build better applications, consider giving it a star ⭐
