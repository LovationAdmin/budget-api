#!/usr/bin/env bash
# ============================================================================
# Budget Famille — Admin re-engagement campaign API
# ============================================================================
# Exactly the same auth model as your existing /admin/stats endpoint:
# pass the ADMIN_SECRET (configured in Render env vars) as X-Admin-Secret.
# ============================================================================

ADMIN_SECRET="paste-your-admin-secret-here"
BASE_URL="https://budget-api-778i.onrender.com/api/v1"   # production
# BASE_URL="http://localhost:8080/api/v1"                # local

# ─── Step 1 — DRY RUN both segments ──────────────────────────────────────────
# Renders + counts targets + logs masked recipients. No emails actually sent.
# Always do this first and verify the totals match what /admin/stats shows.
curl -X POST "$BASE_URL/admin/campaigns/send" \
  -H "X-Admin-Secret: $ADMIN_SECRET" \
  -H "Content-Type: application/json" \
  -d '{
    "campaign_id": "reengagement_2026_05",
    "auto": true,
    "dry_run": true,
    "skip_sent": true
  }' | jq .

# Expected response shape:
# {
#   "campaign_id": "reengagement_2026_05",
#   "auto": true,
#   "verified":   { "total": 25, "sent": 25, "skipped": 0, "failed": 0, ... },
#   "unverified": { "total": 12, "sent": 12, "skipped": 0, "failed": 0, ... }
# }


# ─── Step 2 — SMOKE TEST on yourself only ────────────────────────────────────
# Use a unique smoketest campaign_id + limit:1 to send to yourself first
# (the oldest verified user — usually you, since you signed up first).
curl -X POST "$BASE_URL/admin/campaigns/send" \
  -H "X-Admin-Secret: $ADMIN_SECRET" \
  -H "Content-Type: application/json" \
  -d '{
    "campaign_id": "reengagement_2026_05_smoketest",
    "auto": false,
    "variant": "reengagement_verified",
    "segment": "verified",
    "dry_run": false,
    "limit": 1
  }' | jq .

# Then check Gmail / Apple Mail rendering:
#   ✓ Subject line shows correctly
#   ✓ "Libasse — Budget Famille" displayed as sender
#   ✓ Reply lands in lovation.pro@gmail.com (try replying to yourself)
#   ✓ CTA button works and includes the UTM params


# ─── Step 3 — REAL SEND, both segments ───────────────────────────────────────
# Idempotent: skip_sent:true means re-running this is safe.
curl -X POST "$BASE_URL/admin/campaigns/send" \
  -H "X-Admin-Secret: $ADMIN_SECRET" \
  -H "Content-Type: application/json" \
  -d '{
    "campaign_id": "reengagement_2026_05",
    "auto": true,
    "dry_run": false,
    "skip_sent": true
  }' | jq .


# ─── Audit query (run in psql) ───────────────────────────────────────────────
# SELECT campaign_id, status, COUNT(*) AS n
# FROM email_campaign_sends
# WHERE campaign_id LIKE 'reengagement_2026_05%'
# GROUP BY campaign_id, status
# ORDER BY campaign_id, status;
#
#  campaign_id                       | status | n
# -----------------------------------+--------+----
#  reengagement_2026_05_unverified   | sent   | 12
#  reengagement_2026_05_verified     | sent   | 25
