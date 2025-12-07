package scrapers

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// ETCScraper handles web scraping for ETC meisai service (etc-meisai.jp)
type ETCScraper struct {
	BaseScraper
}

// NewETCScraper creates a new ETC scraper instance
func NewETCScraper(config *ScraperConfig, logger *log.Logger) (Scraper, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "[ETC-SCRAPER] ", log.LstdFlags)
	}

	return &ETCScraper{
		BaseScraper: BaseScraper{
			Config:       config,
			Logger:       logger,
			DownloadDone: make(chan string, 1),
		},
	}, nil
}

// Initialize sets up chromedp browser
func (s *ETCScraper) Initialize() error {
	s.Logger.Println("Initializing browser...")

	if err := os.MkdirAll(s.Config.DownloadPath, 0755); err != nil {
		return fmt.Errorf("failed to create download directory: %w", err)
	}

	absDownloadPath, err := filepath.Abs(s.Config.DownloadPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}
	s.DownloadPath = absDownloadPath

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", s.Config.Headless),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.WindowSize(1920, 1080),
	)

	if s.Config.Headless {
		s.Logger.Println("Running in HEADLESS mode")
	} else {
		s.Logger.Println("Running in VISIBLE mode")
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(s.Logger.Printf))

	s.Ctx = ctx
	s.Cancel = cancel
	s.AllocCancel = allocCancel

	// ブラウザ全体でダウンロードを許可（新しいタブでも有効）
	if err := chromedp.Run(s.Ctx,
		browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).
			WithDownloadPath(absDownloadPath).
			WithEventsEnabled(true),
	); err != nil {
		return fmt.Errorf("failed to set download behavior: %w", err)
	}

	// ブラウザレベルでダウンロードイベントを監視（新しいタブを含む）
	chromedp.ListenBrowser(s.Ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *browser.EventDownloadProgress:
			s.Logger.Printf("Browser download event: GUID=%s State=%s", e.GUID, e.State)
			if e.State == browser.DownloadProgressStateCompleted {
				s.Logger.Printf("Download completed: %s", e.GUID)
				guidFile := filepath.Join(absDownloadPath, e.GUID)
				if _, err := os.Stat(guidFile); err == nil {
					csvFile := guidFile + ".csv"
					os.Rename(guidFile, csvFile)
					s.Logger.Printf("Renamed to: %s", csvFile)
					select {
					case s.DownloadDone <- csvFile:
					default:
					}
				} else {
					files, _ := filepath.Glob(filepath.Join(absDownloadPath, "*"))
					for _, f := range files {
						if filepath.Base(f) == e.GUID {
							csvFile := f + ".csv"
							os.Rename(f, csvFile)
							select {
							case s.DownloadDone <- csvFile:
							default:
							}
							break
						}
					}
				}
			}
		case *target.EventTargetCreated:
			// 新しいタブが作成されたら、そのタブでもダウンロードを許可
			s.Logger.Printf("New target created: %s (type: %s)", e.TargetInfo.TargetID, e.TargetInfo.Type)
		}
	})

	// ターゲットレベルのイベント（ダイアログ等）
	chromedp.ListenTarget(s.Ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *page.EventJavascriptDialogOpening:
			s.Logger.Printf("Dialog: %s", e.Message)
			go chromedp.Run(s.Ctx, page.HandleJavaScriptDialog(true))
		}
	})

	s.Logger.Printf("Browser initialized. Download path: %s", absDownloadPath)
	return nil
}

// Login performs login to ETC meisai service
func (s *ETCScraper) Login() error {
	s.Logger.Println("Navigating to https://www.etc-meisai.jp/")

	if err := chromedp.Run(s.Ctx,
		chromedp.Navigate("https://www.etc-meisai.jp/"),
		chromedp.WaitReady("body"),
	); err != nil {
		return fmt.Errorf("failed to navigate: %w", err)
	}

	s.Logger.Println("Clicking login link...")
	if err := chromedp.Run(s.Ctx,
		chromedp.WaitVisible(`a[href*='funccode=1013000000']`),
		chromedp.Click(`a[href*='funccode=1013000000']`),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		return fmt.Errorf("failed to click login link: %w", err)
	}

	s.Logger.Printf("Filling credentials for user: %s", s.Config.UserID)
	if err := chromedp.Run(s.Ctx,
		chromedp.WaitVisible(`input[name='risLoginId']`),
		chromedp.SendKeys(`input[name='risLoginId']`, s.Config.UserID),
		chromedp.SendKeys(`input[name='risPassword']`, s.Config.Password),
	); err != nil {
		return fmt.Errorf("failed to fill credentials: %w", err)
	}

	s.Logger.Println("Clicking login button...")
	if err := chromedp.Run(s.Ctx,
		chromedp.Click(`input[type='button'][value='ログイン']`),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		return fmt.Errorf("failed to click login: %w", err)
	}

	s.Logger.Println("Login completed!")
	return nil
}

// Download downloads ETC meisai CSV
func (s *ETCScraper) Download() (string, error) {
	s.Logger.Println("Starting download process...")

	s.Logger.Println("Navigating to search page...")
	if err := chromedp.Run(s.Ctx,
		chromedp.Evaluate(`
			(function() {
				var links = document.querySelectorAll('a');
				for (var i = 0; i < links.length; i++) {
					if (links[i].textContent.indexOf('検索条件の指定') >= 0) {
						links[i].click();
						return true;
					}
				}
				return false;
			})()
		`, nil),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		s.Logger.Printf("Warning: %v", err)
	}

	s.Logger.Println("Selecting '全て' option...")
	chromedp.Run(s.Ctx,
		chromedp.Click(`input[name='sokoKbn'][value='0']`, chromedp.NodeVisible),
		chromedp.Sleep(1*time.Second),
	)

	s.Logger.Println("Saving settings...")
	chromedp.Run(s.Ctx,
		chromedp.Click(`input[name='focusTarget_Save']`, chromedp.NodeVisible),
		chromedp.Sleep(2*time.Second),
	)

	s.Logger.Println("Clicking search button...")
	if err := chromedp.Run(s.Ctx,
		chromedp.Click(`input[name='focusTarget']`, chromedp.NodeVisible),
		chromedp.Sleep(3*time.Second),
		// ページが完全に読み込まれるまで待つ
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		return "", fmt.Errorf("failed to search: %w", err)
	}

	// JavaScriptが完全に読み込まれるまでポーリングで待つ
	s.Logger.Println("Waiting for page scripts to load...")
	for i := 0; i < 30; i++ { // 最大30秒待つ
		var ready bool
		chromedp.Run(s.Ctx,
			chromedp.Evaluate(`
				(typeof goOutput === 'function' && typeof submitOpenPage === 'function')
			`, &ready),
		)
		if ready {
			s.Logger.Println("All scripts loaded!")
			break
		}
		s.Logger.Printf("Waiting for scripts... (%d/30)", i+1)
		time.Sleep(1 * time.Second)
	}

	// ページ上のリンクをデバッグ出力
	var allLinks string
	chromedp.Run(s.Ctx,
		chromedp.Evaluate(`
			(function() {
				var links = document.querySelectorAll('a');
				var texts = [];
				for (var i = 0; i < links.length; i++) {
					texts.push(links[i].textContent.trim());
				}
				return texts.join(' | ');
			})()
		`, &allLinks),
	)
	s.Logger.Printf("All links on page: %s", allLinks)

	s.Logger.Println("Clicking CSV download link...")

	// CSVリンクをクリック
	var found bool
	chromedp.Run(s.Ctx,
		chromedp.Evaluate(`
			(function() {
				var links = document.querySelectorAll('a');
				for (var i = 0; i < links.length; i++) {
					var text = links[i].textContent;
					if (text.indexOf('明細') >= 0 && (text.indexOf('CSV') >= 0 || text.indexOf('ＣＳＶ') >= 0)) {
						console.log('Found CSV link: ' + text);
						links[i].click();
						return true;
					}
				}
				return false;
			})()
		`, &found),
	)
	s.Logger.Printf("CSV link clicked: %v", found)

	// ダウンロード完了をポーリングで待つ（最大30秒）
	s.Logger.Println("Waiting for download...")
	for i := 0; i < 30; i++ {
		select {
		case path := <-s.DownloadDone:
			s.Logger.Printf("Downloaded (event): %s", path)
			return path, nil
		default:
		}

		// ファイルが存在するかチェック
		allFiles, _ := filepath.Glob(filepath.Join(s.DownloadPath, "*"))
		for _, f := range allFiles {
			info, err := os.Stat(f)
			if err != nil || info.IsDir() {
				continue
			}
			// .csvファイルがあれば完了
			if filepath.Ext(f) == ".csv" {
				s.Logger.Printf("Found CSV file: %s", f)
				return f, nil
			}
			// 拡張子がないファイル（GUID形式）で十分なサイズがあれば完了
			if filepath.Ext(f) == "" && info.Size() > 100 {
				csvFile := f + ".csv"
				if err := os.Rename(f, csvFile); err == nil {
					s.Logger.Printf("Renamed GUID file to: %s", csvFile)
					return csvFile, nil
				}
			}
		}

		time.Sleep(1 * time.Second)
	}

	return "", fmt.Errorf("download timeout")
}

// Close cleans up resources
func (s *ETCScraper) Close() error {
	if s.Cancel != nil {
		s.Cancel()
	}
	if s.AllocCancel != nil {
		s.AllocCancel()
	}
	return nil
}
