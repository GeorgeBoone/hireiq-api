// internal/service/yahoo_finance.go
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ── Response Types ──────────────────────────────────────

// CompanyIntel is the unified response for both public and private companies
type CompanyIntel struct {
	Company       string          `json:"company"`
	Ticker        string          `json:"ticker,omitempty"`
	IsPublic      bool            `json:"isPublic"`
	Source        string          `json:"source"` // "yahoo_finance" | "ai_estimated"
	FetchedAt     time.Time       `json:"fetchedAt"`
	Profile       CompanyProfile  `json:"profile"`
	Financials    CompanyFinance  `json:"financials"`
	Ratings       CompanyRatings  `json:"ratings"`
	Earnings      []QuarterData   `json:"earnings"`
	Officers      []Officer       `json:"officers,omitempty"`
}

type CompanyProfile struct {
	Industry          string `json:"industry"`
	Sector            string `json:"sector"`
	FullTimeEmployees int64  `json:"fullTimeEmployees"`
	Website           string `json:"website"`
	City              string `json:"city"`
	Country           string `json:"country"`
	Summary           string `json:"summary"`
	Founded           int    `json:"founded,omitempty"`
}

type CompanyFinance struct {
	MarketCap        int64   `json:"marketCap"`
	MarketCapFmt     string  `json:"marketCapFmt"`
	EnterpriseValue  int64   `json:"enterpriseValue"`
	EnterpriseFmt    string  `json:"enterpriseValueFmt"`
	TotalRevenue     int64   `json:"totalRevenue"`
	TotalRevenueFmt  string  `json:"totalRevenueFmt"`
	RevenueGrowth    float64 `json:"revenueGrowth"`
	GrossMargins     float64 `json:"grossMargins"`
	OperatingMargins float64 `json:"operatingMargins"`
	ProfitMargins    float64 `json:"profitMargins"`
	TrailingPE       float64 `json:"trailingPE"`
	ForwardPE        float64 `json:"forwardPE"`
	CurrentPrice     float64 `json:"currentPrice"`
	TargetMeanPrice  float64 `json:"targetMeanPrice"`
	DividendYield    float64 `json:"dividendYield"`
	DebtToEquity     float64 `json:"debtToEquity"`
	FreeCashflow     int64   `json:"freeCashflow"`
	FreeCashflowFmt  string  `json:"freeCashflowFmt"`
}

type CompanyRatings struct {
	OverallRisk    int     `json:"overallRisk"`    // 1-10 governance
	AuditRisk      int     `json:"auditRisk"`
	BoardRisk      int     `json:"boardRisk"`
	CompensationRisk int   `json:"compensationRisk"`
	ShareholderRisk int    `json:"shareholderRisk"`
	RecommendationMean float64 `json:"recommendationMean"` // 1=strong buy, 5=sell
	RecommendationKey  string  `json:"recommendationKey"`
	NumberOfAnalysts   int     `json:"numberOfAnalysts"`
	TargetHighPrice    float64 `json:"targetHighPrice"`
	TargetLowPrice     float64 `json:"targetLowPrice"`
}

type QuarterData struct {
	Quarter  string  `json:"quarter"`
	Revenue  int64   `json:"revenue"`
	Earnings int64   `json:"earnings"`
}

type Officer struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Age   int    `json:"age,omitempty"`
}

// ── Yahoo Finance Client ────────────────────────────────

type YahooFinanceClient struct {
	client   *http.Client
	cache    map[string]*cachedIntel
	mu       sync.RWMutex
	crumb    string
	crumbMu  sync.Mutex
	crumbExp time.Time
}

type cachedIntel struct {
	data      *CompanyIntel
	expiresAt time.Time
}

const (
	yahooBaseURL = "https://query2.finance.yahoo.com"
	cacheTTL     = 6 * time.Hour
	crumbTTL     = 1 * time.Hour
	userAgent    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

func NewYahooFinanceClient() *YahooFinanceClient {
	jar, _ := cookiejar.New(nil)
	return &YahooFinanceClient{
		client: &http.Client{
			Timeout: 15 * time.Second,
			Jar:     jar,
		},
		cache: make(map[string]*cachedIntel),
	}
}

// getCrumb fetches a fresh crumb token from Yahoo Finance.
// Yahoo requires: 1) visit a page to get session cookies, 2) fetch crumb with those cookies.
// The crumb is cached for 1 hour; the cookie jar persists on the http.Client.
func (yf *YahooFinanceClient) getCrumb(ctx context.Context) (string, error) {
	yf.crumbMu.Lock()
	defer yf.crumbMu.Unlock()

	// Return cached crumb if still valid
	if yf.crumb != "" && time.Now().Before(yf.crumbExp) {
		return yf.crumb, nil
	}

	// Step 1: Hit the Yahoo Finance consent/landing page to establish cookies
	seedURL := "https://fc.yahoo.com"
	seedReq, err := http.NewRequestWithContext(ctx, "GET", seedURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating seed request: %w", err)
	}
	seedReq.Header.Set("User-Agent", userAgent)
	seedResp, err := yf.client.Do(seedReq)
	if err != nil {
		return "", fmt.Errorf("seed request failed: %w", err)
	}
	seedResp.Body.Close()
	// We don't care about the status — we just need the cookies in the jar

	// Step 2: Fetch the crumb using the session cookies
	crumbURL := "https://query2.finance.yahoo.com/v1/test/getcrumb"
	crumbReq, err := http.NewRequestWithContext(ctx, "GET", crumbURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating crumb request: %w", err)
	}
	crumbReq.Header.Set("User-Agent", userAgent)

	crumbResp, err := yf.client.Do(crumbReq)
	if err != nil {
		return "", fmt.Errorf("crumb request failed: %w", err)
	}
	defer crumbResp.Body.Close()

	crumbBody, err := io.ReadAll(crumbResp.Body)
	if err != nil {
		return "", fmt.Errorf("reading crumb response: %w", err)
	}

	if crumbResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("crumb endpoint returned %d: %s", crumbResp.StatusCode, string(crumbBody))
	}

	crumb := strings.TrimSpace(string(crumbBody))
	if crumb == "" {
		return "", fmt.Errorf("empty crumb returned")
	}

	yf.crumb = crumb
	yf.crumbExp = time.Now().Add(crumbTTL)

	log.Debug().Str("crumb", crumb[:min(8, len(crumb))]+"...").Msg("Yahoo Finance crumb obtained")

	return crumb, nil
}

// FetchCompanyIntel retrieves company data from Yahoo Finance quoteSummary API
func (yf *YahooFinanceClient) FetchCompanyIntel(ctx context.Context, ticker string) (*CompanyIntel, error) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if ticker == "" {
		return nil, fmt.Errorf("ticker is required")
	}

	// Check cache first
	yf.mu.RLock()
	if cached, ok := yf.cache[ticker]; ok && time.Now().Before(cached.expiresAt) {
		yf.mu.RUnlock()
		log.Debug().Str("ticker", ticker).Msg("Yahoo Finance cache hit")
		return cached.data, nil
	}
	yf.mu.RUnlock()

	// Try fetching, with one retry on auth failure (stale crumb)
	intel, err := yf.fetchWithCrumb(ctx, ticker)
	if err != nil && strings.Contains(err.Error(), "401") {
		// Crumb expired — invalidate and retry once
		log.Debug().Str("ticker", ticker).Msg("Crumb expired, refreshing and retrying")
		yf.crumbMu.Lock()
		yf.crumb = ""
		yf.crumbExp = time.Time{}
		yf.crumbMu.Unlock()

		intel, err = yf.fetchWithCrumb(ctx, ticker)
	}
	if err != nil {
		return nil, err
	}

	// Cache the result
	yf.mu.Lock()
	yf.cache[ticker] = &cachedIntel{
		data:      intel,
		expiresAt: time.Now().Add(cacheTTL),
	}
	yf.mu.Unlock()

	log.Info().Str("ticker", ticker).Str("company", intel.Company).Msg("Yahoo Finance data fetched and cached")

	return intel, nil
}

// fetchWithCrumb performs the actual quoteSummary API call with crumb authentication
func (yf *YahooFinanceClient) fetchWithCrumb(ctx context.Context, ticker string) (*CompanyIntel, error) {
	crumb, err := yf.getCrumb(ctx)
	if err != nil {
		return nil, fmt.Errorf("obtaining crumb: %w", err)
	}

	modules := "assetProfile,financialData,defaultKeyStatistics,summaryDetail,price,earnings,recommendationTrend"
	url := fmt.Sprintf("%s/v10/finance/quoteSummary/%s?modules=%s&crumb=%s",
		yahooBaseURL, ticker, modules, crumb)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := yf.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching Yahoo Finance data: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Yahoo Finance returned %d: %s", resp.StatusCode, truncateBytes(body, 200))
	}

	// Parse the raw JSON response
	intel, err := parseYahooResponse(ticker, body)
	if err != nil {
		return nil, fmt.Errorf("parsing Yahoo Finance response: %w", err)
	}

	return intel, nil
}

// SearchTicker attempts to find a ticker symbol for a company name
func (yf *YahooFinanceClient) SearchTicker(ctx context.Context, companyName string) (string, error) {
	url := fmt.Sprintf("https://query2.finance.yahoo.com/v1/finance/search?q=%s&quotesCount=5&newsCount=0",
		strings.ReplaceAll(companyName, " ", "+"))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating search request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := yf.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("searching Yahoo Finance: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading search response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Yahoo search returned %d", resp.StatusCode)
	}

	var searchResp struct {
		Quotes []struct {
			Symbol   string `json:"symbol"`
			ShortName string `json:"shortname"`
			QuoteType string `json:"quoteType"`
			Exchange  string `json:"exchange"`
		} `json:"quotes"`
	}

	if err := json.Unmarshal(body, &searchResp); err != nil {
		return "", fmt.Errorf("parsing search results: %w", err)
	}

	// Find the best match — prefer EQUITY type on major exchanges
	for _, q := range searchResp.Quotes {
		if q.QuoteType == "EQUITY" {
			log.Debug().
				Str("company", companyName).
				Str("ticker", q.Symbol).
				Str("name", q.ShortName).
				Msg("Ticker found via Yahoo search")
			return q.Symbol, nil
		}
	}

	// Fallback to first result if no equity match
	if len(searchResp.Quotes) > 0 {
		return searchResp.Quotes[0].Symbol, nil
	}

	return "", fmt.Errorf("no ticker found for %q", companyName)
}

// ClearCache removes expired entries
func (yf *YahooFinanceClient) ClearCache() {
	yf.mu.Lock()
	defer yf.mu.Unlock()
	now := time.Now()
	for k, v := range yf.cache {
		if now.After(v.expiresAt) {
			delete(yf.cache, k)
		}
	}
}

// ── Yahoo Finance JSON Parsing ──────────────────────────

func parseYahooResponse(ticker string, body []byte) (*CompanyIntel, error) {
	var raw struct {
		QuoteSummary struct {
			Result []json.RawMessage `json:"result"`
			Error  *struct {
				Code        string `json:"code"`
				Description string `json:"description"`
			} `json:"error"`
		} `json:"quoteSummary"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshaling wrapper: %w", err)
	}

	if raw.QuoteSummary.Error != nil {
		return nil, fmt.Errorf("yahoo error: %s — %s", raw.QuoteSummary.Error.Code, raw.QuoteSummary.Error.Description)
	}

	if len(raw.QuoteSummary.Result) == 0 {
		return nil, fmt.Errorf("no results returned for %s", ticker)
	}

	// Parse the modules from the first result
	var modules map[string]json.RawMessage
	if err := json.Unmarshal(raw.QuoteSummary.Result[0], &modules); err != nil {
		return nil, fmt.Errorf("unmarshaling modules: %w", err)
	}

	intel := &CompanyIntel{
		Ticker:   ticker,
		IsPublic: true,
		Source:   "yahoo_finance",
		FetchedAt: time.Now(),
	}

	// Parse assetProfile
	if data, ok := modules["assetProfile"]; ok {
		var ap struct {
			Industry          string `json:"industry"`
			Sector            string `json:"sector"`
			FullTimeEmployees int64  `json:"fullTimeEmployees"`
			Website           string `json:"website"`
			City              string `json:"city"`
			Country           string `json:"country"`
			LongBusinessSummary string `json:"longBusinessSummary"`
			AuditRisk         int    `json:"auditRisk"`
			BoardRisk         int    `json:"boardRisk"`
			CompensationRisk  int    `json:"compensationRisk"`
			ShareHolderRightsRisk int `json:"shareHolderRightsRisk"`
			OverallRisk       int    `json:"overallRisk"`
			CompanyOfficers   []struct {
				Name  string `json:"name"`
				Title string `json:"title"`
				Age   int    `json:"age"`
			} `json:"companyOfficers"`
		}
		if err := json.Unmarshal(data, &ap); err == nil {
			intel.Profile = CompanyProfile{
				Industry:          ap.Industry,
				Sector:            ap.Sector,
				FullTimeEmployees: ap.FullTimeEmployees,
				Website:           ap.Website,
				City:              ap.City,
				Country:           ap.Country,
				Summary:           truncateString(ap.LongBusinessSummary, 500),
			}
			intel.Ratings.AuditRisk = ap.AuditRisk
			intel.Ratings.BoardRisk = ap.BoardRisk
			intel.Ratings.CompensationRisk = ap.CompensationRisk
			intel.Ratings.ShareholderRisk = ap.ShareHolderRightsRisk
			intel.Ratings.OverallRisk = ap.OverallRisk

			for _, o := range ap.CompanyOfficers {
				if len(intel.Officers) >= 5 {
					break
				}
				intel.Officers = append(intel.Officers, Officer{
					Name:  o.Name,
					Title: o.Title,
					Age:   o.Age,
				})
			}
		}
	}

	// Parse price module for company name and market cap
	if data, ok := modules["price"]; ok {
		var p struct {
			ShortName        string `json:"shortName"`
			LongName         string `json:"longName"`
			MarketCap        yfVal  `json:"marketCap"`
			RegularMarketPrice yfVal `json:"regularMarketPrice"`
			Currency         string `json:"currency"`
		}
		if err := json.Unmarshal(data, &p); err == nil {
			if p.LongName != "" {
				intel.Company = p.LongName
			} else {
				intel.Company = p.ShortName
			}
			intel.Financials.MarketCap = int64(p.MarketCap.Raw)
			intel.Financials.MarketCapFmt = p.MarketCap.Fmt
			intel.Financials.CurrentPrice = p.RegularMarketPrice.Raw
		}
	}

	// Parse financialData
	if data, ok := modules["financialData"]; ok {
		var fd struct {
			TotalRevenue     yfVal   `json:"totalRevenue"`
			RevenueGrowth    yfVal   `json:"revenueGrowth"`
			GrossMargins     yfVal   `json:"grossMargins"`
			OperatingMargins yfVal   `json:"operatingMargins"`
			ProfitMargins    yfVal   `json:"profitMargins"`
			CurrentPrice     yfVal   `json:"currentPrice"`
			TargetMeanPrice  yfVal   `json:"targetMeanPrice"`
			TargetHighPrice  yfVal   `json:"targetHighPrice"`
			TargetLowPrice   yfVal   `json:"targetLowPrice"`
			RecommendationMean yfVal `json:"recommendationMean"`
			RecommendationKey  string `json:"recommendationKey"`
			NumberOfAnalystOpinions yfVal `json:"numberOfAnalystOpinions"`
			FreeCashflow     yfVal   `json:"freeCashflow"`
			DebtToEquity     yfVal   `json:"debtToEquity"`
		}
		if err := json.Unmarshal(data, &fd); err == nil {
			intel.Financials.TotalRevenue = int64(fd.TotalRevenue.Raw)
			intel.Financials.TotalRevenueFmt = fd.TotalRevenue.Fmt
			intel.Financials.RevenueGrowth = fd.RevenueGrowth.Raw
			intel.Financials.GrossMargins = fd.GrossMargins.Raw
			intel.Financials.OperatingMargins = fd.OperatingMargins.Raw
			intel.Financials.ProfitMargins = fd.ProfitMargins.Raw
			intel.Financials.TargetMeanPrice = fd.TargetMeanPrice.Raw
			intel.Financials.FreeCashflow = int64(fd.FreeCashflow.Raw)
			intel.Financials.FreeCashflowFmt = fd.FreeCashflow.Fmt
			intel.Financials.DebtToEquity = fd.DebtToEquity.Raw
			if fd.CurrentPrice.Raw > 0 {
				intel.Financials.CurrentPrice = fd.CurrentPrice.Raw
			}
			intel.Ratings.RecommendationMean = fd.RecommendationMean.Raw
			intel.Ratings.RecommendationKey = fd.RecommendationKey
			intel.Ratings.NumberOfAnalysts = int(fd.NumberOfAnalystOpinions.Raw)
			intel.Ratings.TargetHighPrice = fd.TargetHighPrice.Raw
			intel.Ratings.TargetLowPrice = fd.TargetLowPrice.Raw
		}
	}

	// Parse defaultKeyStatistics
	if data, ok := modules["defaultKeyStatistics"]; ok {
		var ks struct {
			EnterpriseValue yfVal `json:"enterpriseValue"`
			TrailingPE      yfVal `json:"trailingPE"` // sometimes here instead of summaryDetail
			ForwardPE       yfVal `json:"forwardPE"`
		}
		if err := json.Unmarshal(data, &ks); err == nil {
			intel.Financials.EnterpriseValue = int64(ks.EnterpriseValue.Raw)
			intel.Financials.EnterpriseFmt = ks.EnterpriseValue.Fmt
			if ks.ForwardPE.Raw > 0 {
				intel.Financials.ForwardPE = ks.ForwardPE.Raw
			}
			if ks.TrailingPE.Raw > 0 {
				intel.Financials.TrailingPE = ks.TrailingPE.Raw
			}
		}
	}

	// Parse summaryDetail for PE ratios and dividend yield
	if data, ok := modules["summaryDetail"]; ok {
		var sd struct {
			TrailingPE    yfVal `json:"trailingPE"`
			ForwardPE     yfVal `json:"forwardPE"`
			DividendYield yfVal `json:"dividendYield"`
		}
		if err := json.Unmarshal(data, &sd); err == nil {
			if sd.TrailingPE.Raw > 0 {
				intel.Financials.TrailingPE = sd.TrailingPE.Raw
			}
			if sd.ForwardPE.Raw > 0 {
				intel.Financials.ForwardPE = sd.ForwardPE.Raw
			}
			intel.Financials.DividendYield = sd.DividendYield.Raw
		}
	}

	// Parse earnings for quarterly data (sparkline charts)
	if data, ok := modules["earnings"]; ok {
		var e struct {
			FinancialsChart struct {
				Quarterly []struct {
					Date     string `json:"date"`
					Revenue  yfVal  `json:"revenue"`
					Earnings yfVal  `json:"earnings"`
				} `json:"quarterly"`
			} `json:"financialsChart"`
		}
		if err := json.Unmarshal(data, &e); err == nil {
			for _, q := range e.FinancialsChart.Quarterly {
				intel.Earnings = append(intel.Earnings, QuarterData{
					Quarter:  q.Date,
					Revenue:  int64(q.Revenue.Raw),
					Earnings: int64(q.Earnings.Raw),
				})
			}
		}
	}

	return intel, nil
}

// ── Yahoo Finance value wrapper (raw + formatted) ───────

type yfVal struct {
	Raw float64 `json:"raw"`
	Fmt string  `json:"fmt"`
}

// ── Helpers ─────────────────────────────────────────────

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Cut at last space before maxLen to avoid chopping words
	if idx := strings.LastIndex(s[:maxLen], " "); idx > maxLen/2 {
		return s[:idx] + "..."
	}
	return s[:maxLen] + "..."
}

func truncateBytes(b []byte, maxLen int) string {
	if len(b) <= maxLen {
		return string(b)
	}
	return string(b[:maxLen]) + "..."
}
