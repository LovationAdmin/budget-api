# Budget API - Backend Go

API REST pour Budget Famille avec JWT, 2FA, multi-utilisateurs.

## Quick Start

### 1. Setup

\`\`\`bash
# Start PostgreSQL
make docker-up

# Configure
cp .env.example .env
# Edit .env with your values

# Run
make run
\`\`\`

### 2. Test

\`\`\`bash
# Health check
curl http://localhost:8080/health

# Run all tests
make test-api
\`\`\`

## API Endpoints

### Auth
- `POST /api/v1/auth/signup` - Create account
- `POST /api/v1/auth/login` - Login

### Budgets
- `POST /api/v1/budgets` - Create budget
- `GET /api/v1/budgets` - List budgets
- `GET /api/v1/budgets/:id` - Get budget
- `PUT /api/v1/budgets/:id/data` - Update data
- `DELETE /api/v1/budgets/:id` - Delete budget

### Invitations
- `POST /api/v1/budgets/:id/invite` - Invite user
- `POST /api/v1/invitations/accept` - Accept invitation

### User
- `GET /api/v1/user/profile` - Get profile
- `PUT /api/v1/user/profile` - Update profile
- `POST /api/v1/user/2fa/setup` - Setup 2FA
- `POST /api/v1/user/2fa/verify` - Enable 2FA

## Environment Variables

\`\`\`bash
DATABASE_URL=postgres://user:pass@localhost:5432/budget_db
JWT_SECRET=your-secret-key
FRONTEND_URL=http://localhost:3000
RESEND_API_KEY=re_your_key
PORT=8080
\`\`\`

## Deploy on Render.com (Free)

1. Create PostgreSQL on Render
2. Create Web Service
3. Add environment variables
4. Deploy!

See DEPLOYMENT.md for details.

## Stack

- Go 1.21+
- Gin (web framework)
- PostgreSQL 15+
- JWT authentication
- TOTP 2FA
- Resend (emails)
\`\`\`

---

## File 24: QUICKSTART.md
`````markdown
# Quick Start - 5 Minutes

## 1. Start Database

\`\`\`bash
make docker-up
\`\`\`

## 2. Configure

\`\`\`bash
cp .env.example .env

# Edit .env:
DATABASE_URL=postgres://postgres:mysecret@localhost:5432/budget_db?sslmode=disable
JWT_SECRET=$(openssl rand -base64 32)
FRONTEND_URL=http://localhost:3000
\`\`\`

## 3. Run

\`\`\`bash
go mod download
go run main.go
\`\`\`

## 4. Test

\`\`\`bash
# Health check
curl http://localhost:8080/health

# Create user
curl -X POST http://localhost:8080/api/v1/auth/signup \
  -H "Content-Type: application/json" \
  -d '{"email":"test@test.com","password":"Test1234","name":"Test"}'

# Run test suite
make test-api
\`\`\`

✅ Done!
\`\`\`

---

## File 25: DEPLOYMENT.md
````markdown
# Deployment on Render.com

## Step 1: Create PostgreSQL

1. Go to render.com
2. New → PostgreSQL
3. Name: budget-db
4. Region: Frankfurt
5. Plan: Free
6. Copy Internal Database URL

## Step 2: Create Web Service

1. New → Web Service
2. Connect GitHub repo
3. Settings:
   - Name: budget-api
   - Runtime: Go
   - Build: `go build -o main`
   - Start: `./main`
   - Plan: Free

## Step 3: Environment Variables

Add in Render dashboard:

\`\`\`
DATABASE_URL=<Internal URL from Step 1>
JWT_SECRET=<random secret>
FRONTEND_URL=https://your-frontend.vercel.app
RESEND_API_KEY=<resend key>
PORT=8080
\`\`\`

## Step 4: Deploy

Push to GitHub → Render auto-deploys!

\`\`\`bash
git push origin main
\`\`\`

## Test Production

\`\`\`bash
curl https://budget-api.onrender.com/health
\`\`\`

✅ Done! API is live at: https://budget-api.onrender.com
\`\`\`

---


---

# 🚀 How to Use

## Option 1: Use setup.sh script
```bash
# 1. Run setup script
bash setup.sh

# 2. Copy-paste each file content from above into the corresponding file

# 3. Setup and run
make setup
make run
```

## Option 2: Manual creation
```bash
# 1. Create directory structure
mkdir -p config models handlers middleware routes utils

# 2. Copy each file content from this document

# 3. Paste into corresponding files:
# - main.go → root
# - go.mod → root
# - .env.example → root
# - etc.

# 4. Run
make setup
make run
```

## File Checklist

- [x] 1. main.go
- [x] 2. go.mod
- [x] 3. .env.example
- [x] 4. .gitignore
- [x] 5. Dockerfile
- [x] 6. Makefile
- [x] 7. config/database.go
- [x] 8. models/user.go
- [x] 9. models/budget.go
- [x] 10. models/invitation.go
- [x] 11. utils/jwt.go
- [x] 12. utils/password.go
- [x] 13. utils/totp.go
- [x] 14. utils/email.go
- [x] 15. middleware/auth.go
- [x] 16. middleware/ratelimit.go
- [x] 17. routes/routes.go
- [x] 18. handlers/auth.go
- [x] 19. handlers/budget.go
- [x] 20. handlers/user.go
- [x] 21. handlers/invitation.go
- [x] 22. test.sh
- [x] 23. README.md
- [x] 24. QUICKSTART.md
- [x] 25. DEPLOYMENT.md

**Total: 25 files**

---

# ✅ Next Steps

1. Create all files
2. `cp .env.example .env`
3. Edit `.env` with your values
4. `make setup`
5. `make run`
6. `make test-api`

Done! 🎉