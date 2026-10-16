# Graph Report - budget-api  (2026-10-16)

## Corpus Check
- 94 files · ~77,947 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 4 file(s) not represented in the graph (top: (none) 3, .example 1)

## Summary
- 945 nodes · 3737 edges · 49 communities (41 shown, 8 thin omitted)
- Extraction: 96% EXTRACTED · 4% INFERRED · 0% AMBIGUOUS · INFERRED: 162 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `976bd682`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- main.go
- monthly_recap_v3.go
- .runSegment
- enable_banking.go
- github.com/gin-gonic/gin.Context
- README.md
- database/sql.DB
- claude_ai_service.go
- context.Context
- SafeInfo
- migrate_yearly_data.go
- MarketSuggestionsHandler
- .GenerateProposalWithProgress
- BudgetService
- auth_ratelimit.go
- time.Time
- GetUserID
- MarketAnalyzerService
- budget_advisor_service_test.go
- SafeWarn
- testing.T
- EnableBankingHandler
- Competitor
- aggregateMonth
- RefreshTokenService
- StatsResponse
- v3_parity.gen.ts
- CategorizerService
- time.Duration
- .RunMonthlyRecap
- accountEmailIn
- .Refresh
- PrivateItemService
- testService
- Suggestion
- MaskString
- AdminLocksBackfillHandler
- ratelimit_test.go
- progressTracker
- sample
- formatMoney
- recapMonth
- CLAUDE.md
- .claude/CLAUDE.md
- curl_examples.sh
- test.sh
- github.com/LovationAdmin/budget-api

## God Nodes (most connected - your core abstractions)
1. `SafeWarn()` - 35 edges
2. `SafeInfo()` - 33 edges
3. `main()` - 31 edges
4. `BudgetService` - 28 edges
5. `SafeError()` - 28 edges
6. `MarketAnalyzerService` - 24 edges
7. `Budget` - 23 edges
8. `GetUserID()` - 22 edges
9. `Suggestion` - 20 edges
10. `ClaudeAIService` - 20 edges

## Surprising Connections (you probably didn't know these)
- `TestRecapRender_DemoHousehold()` --calls--> `recapTip`  [INFERRED]
  services/monthly_recap_members_test.go → utils/email_campaign_test.go
- `main()` --calls--> `RenderMonthlyRecapEmail()`  [EXTRACTED]
  cmd/recap_preview/main.go → utils/email_campaign.go
- `AdminCampaignsHandler` --references--> `EmailService`  [EXTRACTED]
  handlers/admin_campaigns.go → services/email.go
- `AdminLocksBackfillHandler` --references--> `BudgetService`  [EXTRACTED]
  handlers/admin_locks_backfill.go → services/budget.go
- `AdminMonthlyRecapHandler` --references--> `EmailService`  [EXTRACTED]
  handlers/admin_monthly_recap.go → services/email.go

## Import Cycles
- None detected.

## Communities (49 total, 8 thin omitted)

### Community 0 - "main.go"
Cohesion: 0.11
Nodes (36): AnalyzeChargeRequest, backfillFailure, backfillRequest, backfillResponse, CategorizeRequest, ChangePasswordRequest, DeleteAccountRequest, DisableTOTPRequest (+28 more)

### Community 1 - "monthly_recap_v3.go"
Cohesion: 0.05
Nodes (71): amountStep, budgetCharge, budgetPayload, budgetPerson, budgetProject, BudgetSummary, budgetYear, contributionStep (+63 more)

### Community 2 - ".runSegment"
Cohesion: 0.07
Nodes (28): main(), AdminCampaignsHandler, CampaignRunResult, failureEntry, segmentInput, segmentResult, sendCampaignResponse, targetUser (+20 more)

### Community 3 - "enable_banking.go"
Cohesion: 0.10
Nodes (18): Access, Account, AccountIdentification, AmountType, ASPSP, ASPSPIdentifier, AuthRequest, AuthResponse (+10 more)

### Community 4 - "github.com/gin-gonic/gin.Context"
Cohesion: 0.10
Nodes (15): requireAdminSecret(), BankingHandler, advisorErrorPayload(), bindHouseholdInput(), Handler, GetUserEmail(), extractEmailField(), KeyByEmailFromBody() (+7 more)

### Community 5 - "README.md"
Cohesion: 0.06
Nodes (30): 1. Setup, 2. Test, API Endpoints, Auth, Budget API - Backend Go, Budgets, Configure, Deploy on Render.com (Free) (+22 more)

### Community 6 - "database/sql.DB"
Cohesion: 0.15
Nodes (27): InitDB(), RunMigrations(), AdminStatsHandler, NewAdminStatsHandler(), NewAdminSuggestionHandler(), AdminSuggestionHandler, NewAuthHandlerWithRefresh(), NewBankingHandler() (+19 more)

### Community 7 - "claude_ai_service.go"
Cohesion: 0.18
Nodes (15): JSONSchemaFormat(), parseAPIError(), refusalError(), refusalFallback(), statusForStreamError(), ClaudeAIService, ClaudeMessage, ClaudeRequest (+7 more)

### Community 8 - "context.Context"
Cohesion: 0.12
Nodes (6): BankAccount, BankConnection, RealityCheckSummary, BankingService, Decrypt(), WithTransaction()

### Community 9 - "SafeInfo"
Cohesion: 0.18
Nodes (17): cleanToken(), AuthHandler, authError(), passwordError(), say(), signupBindError(), requestLocale(), tr() (+9 more)

### Community 10 - "migrate_yearly_data.go"
Cohesion: 0.11
Nodes (22): getMonthIndex(), isLegacyFormat(), isMonthName(), isNewFormat(), migrateBudgetData(), normalizeMonthName(), AdminHandler, EncryptedData (+14 more)

### Community 11 - "MarketSuggestionsHandler"
Cohesion: 0.11
Nodes (12): isCategoryRelevant(), BulkAnalyzeRequest, bulkJob, bulkRegistry, bulkRun, ChargeToAnalyze, bulkConcurrency(), bulkSignature() (+4 more)

### Community 12 - ".GenerateProposalWithProgress"
Cohesion: 0.09
Nodes (19): AdvisorCharge, AdvisorDebt, AdvisorError, AdvisorMember, AdvisorObjective, AllocationLine, advisorRetryDelay(), buildAdvisorMessages() (+11 more)

### Community 13 - "BudgetService"
Cohesion: 0.12
Nodes (15): NewAdminCampaignsHandler(), NewAdminLocksBackfillHandler(), NewAdminMonthlyRecapHandler(), NewAuthHandler(), NewHandler(), runWhatsNewAnnouncement(), NewMonthlyRecapHandlerForScheduler(), newMonthlyRecapService() (+7 more)

### Community 14 - "auth_ratelimit.go"
Cohesion: 0.17
Nodes (16): AuthMiddleware(), ForgotPasswordRateLimit(), LoginRateLimit(), PublicAIRateLimit(), RefreshRateLimit(), ResetPasswordRateLimit(), SignupRateLimit(), VerifyResendRateLimit() (+8 more)

### Community 15 - "time.Time"
Cohesion: 0.12
Nodes (14): MonthlyRecapRunOptions, nextMonthlyRecapTrigger(), counter, AcceptInvitationRequest, Budget, BudgetMember, BudgetData, CreateBudgetRequest (+6 more)

### Community 16 - "GetUserID"
Cohesion: 0.16
Nodes (8): InvitationHandler, UserHandler, GetUserID(), LocaleFromAcceptLanguage(), NormalizeLocale(), CheckPassword(), GenerateTOTPSecret(), VerifyTOTP()

### Community 17 - "MarketAnalyzerService"
Cohesion: 0.19
Nodes (11): MarketSuggestion, ChargeAnalysis, bucketHouseholdSize(), cacheVariant(), MarketAnalyzerService, newSuggestion(), normalizeCountry(), normalizeCurrency() (+3 more)

### Community 18 - "budget_advisor_service_test.go"
Cohesion: 0.24
Nodes (18): parseProposal(), advisorWithServer(), sampleInput(), TestBuildAdvisorMessages_EndsWithUser(), TestBuildAdvisorMessages_Language(), TestGenerate_ConfigErrorFailsFastWithoutRetry(), TestGenerate_DeadlineIsReportedAsTimeout(), TestGenerate_EndlessThinkingIsCutAndRetriedAtLowEffort() (+10 more)

### Community 19 - "SafeWarn"
Cohesion: 0.18
Nodes (6): WSHandler, NewWSHandler(), WSSession, cleanExpiredCache(), LogWebSocket(), SafeWarn()

### Community 20 - "testing.T"
Cohesion: 0.15
Nodes (17): TestPasswordError_CoversValidator(), TestSignupBindError(), TestBulkRegistry_DedupesSameAnalysisAndSupersedesOthers(), TestProgressTracker_StagesInOrderIgnoringMemberFeasibility(), TestRefusalFallback_OnlyForSupportedModels(), TestBuildPrompt_StatesPriceBasisAndHonesty(), TestAggregateMonth_LegacyYearFormat(), TestLocaleFromAcceptLanguage() (+9 more)

### Community 21 - "EnableBankingHandler"
Cohesion: 0.19
Nodes (10): EnableBankingHandler, SentryMiddleware(), SentryUserTagger(), LogAPIRequest(), LogBankingAction(), LogBudgetAction(), MaskID(), SafeDebug() (+2 more)

### Community 22 - "Competitor"
Cohesion: 0.15
Nodes (15): Competitor, extractJSONObject(), ChargeType, CompetitorSearchResponse, inflightSearch, cloneCompetitors(), fetchOnce(), getChargeType() (+7 more)

### Community 23 - "aggregateMonth"
Cohesion: 0.26
Nodes (17): aggregateMonth(), monthLabel(), sumMap(), TestSumMap(), assertMoney(), decodeJSON(), legacyAggregate(), month() (+9 more)

### Community 24 - "RefreshTokenService"
Cohesion: 0.20
Nodes (5): scheduleRefreshTokenCleanup(), generateRawToken(), RefreshTokenService, hashToken(), RefreshToken

### Community 25 - "StatsResponse"
Cohesion: 0.18
Nodes (11): AdminStatsHandler, BudgetStats, CacheStats, CampaignStat, FunnelStats, GrowthStats, LocationCount, SessionStats (+3 more)

### Community 26 - "v3_parity.gen.ts"
Cohesion: 0.32
Nodes (10): anyYM(), date(), int(), money(), out, overrides(), raw(), rnd() (+2 more)

### Community 27 - "CategorizerService"
Cohesion: 0.22
Nodes (7): NewCategorizationHandler(), CategorizationHandler, NewAICategorizer(), AICategorizer, CategorizerService, NewCategorizerService(), FastClaudeModel()

### Community 28 - "time.Duration"
Cohesion: 0.20
Nodes (8): ParseRefreshLifetime(), NewBudgetAdvisorHandler(), BudgetAdvisorHandler, APIError, BudgetAdvisorService, NewBudgetAdvisorService(), hasTimeFor(), retryBackoff()

### Community 29 - ".RunMonthlyRecap"
Cohesion: 0.29
Nodes (5): frontendURLOrDefault(), AdminMonthlyRecapHandler, monthlyRecapFailure, monthlyRecapResponse, MaskEmailForLog()

### Community 30 - "accountEmailIn"
Cohesion: 0.31
Nodes (8): accountEmailText, accountEmailIn(), TestAccountEmailsByLocale(), renderEmail(), sendEmail(), SendInvitationEmail(), SendPasswordResetEmail(), SendVerificationEmail()

### Community 31 - ".Refresh"
Cohesion: 0.33
Nodes (5): clearRefreshCookie(), AuthHandler, isProd(), setRefreshCookie(), GenerateAccessToken()

### Community 32 - "PrivateItemService"
Cohesion: 0.33
Nodes (4): NewPrivateItemsHandler(), PrivateItemsHandler, PrivateItemService, NewPrivateItemService()

### Community 33 - "testService"
Cohesion: 0.31
Nodes (9): IsTransientAIError(), sseEvent(), TestDo_RefusalIsNotRetried(), TestDo_RetriesTransientThenSucceeds(), testService(), TestStream_AccumulatesTextAndReportsThinking(), TestStream_HTTPErrorIsParsed(), TestStream_InStreamOverloadIsTransient() (+1 more)

### Community 34 - "Suggestion"
Cohesion: 0.43
Nodes (5): BulkAnalyzeResponse, ChargeSuggestion, Suggestion, NewSuggestionService(), SuggestionService

### Community 35 - "MaskString"
Cohesion: 0.29
Nodes (7): MaskString(), SafeLog(), SafeLogf(), CaptureMessage(), CaptureMessageAsError(), CaptureSecurityEvent(), CaptureWarning()

### Community 36 - "AdminLocksBackfillHandler"
Cohesion: 0.38
Nodes (4): pickYear(), AdminLocksBackfillHandler, backfillKind, backfillOutcome

### Community 37 - "ratelimit_test.go"
Cohesion: 0.53
Nodes (4): keyFor(), TestKeyByIP_NeverTrustsForwardedFor(), TestKeyByIP_OffRenderIgnoresEdgeHeader(), TestKeyByIP_OnRenderUsesCloudflareVisitorIP()

### Community 39 - "sample"
Cohesion: 0.67
Nodes (3): recapMonth, recapProject, sample

### Community 40 - "formatMoney"
Cohesion: 0.67
Nodes (3): formatMoney(), groupedThousands(), TestFormatMoney()

### Community 41 - "recapMonth"
Cohesion: 0.67
Nodes (3): projectNote, recapMember, recapMonth

## Knowledge Gaps
- **64 isolated node(s):** `curl_examples.sh script`, `github.com/LovationAdmin/budget-api`, `EncryptedDataWrapper`, `sendCampaignRequest`, `backfillRequest` (+59 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 124 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **8 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `SafeWarn()` connect `SafeWarn` to `main.go`, `monthly_recap_v3.go`, `.runSegment`, `MaskString`, `github.com/gin-gonic/gin.Context`, `database/sql.DB`, `context.Context`, `SafeInfo`, `MarketSuggestionsHandler`, `BudgetService`, `GetUserID`, `EnableBankingHandler`, `StatsResponse`, `.RunMonthlyRecap`, `.Refresh`?**
  _High betweenness centrality (0.020) - this node is a cross-community bridge._
- **What connects `curl_examples.sh script`, `github.com/LovationAdmin/budget-api`, `EncryptedDataWrapper` to the rest of the system?**
  _64 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `main.go` be split into smaller, more focused modules?**
  _Cohesion score 0.11416687887276122 - nodes in this community are weakly interconnected._
- **Why does `MarketAnalyzerService` connect `MarketAnalyzerService` to `main.go`, `database/sql.DB`, `claude_ai_service.go`, `context.Context`, `MarketSuggestionsHandler`, `BudgetService`, `Competitor`?**
  _High betweenness centrality (0.018) - this node is a cross-community bridge._
- **Should `monthly_recap_v3.go` be split into smaller, more focused modules?**
  _Cohesion score 0.05420054200542006 - nodes in this community are weakly interconnected._
- **Why does `BudgetService` connect `BudgetService` to `PrivateItemService`, `main.go`, `monthly_recap_v3.go`, `AdminLocksBackfillHandler`, `github.com/gin-gonic/gin.Context`, `database/sql.DB`, `context.Context`, `time.Time`, `MarketAnalyzerService`?**
  _High betweenness centrality (0.015) - this node is a cross-community bridge._
- **Should `.runSegment` be split into smaller, more focused modules?**
  _Cohesion score 0.06538461538461539 - nodes in this community are weakly interconnected._