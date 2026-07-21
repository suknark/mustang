// Command mustang listens to the microphone and presses a key when you
// snap your fingers — like a certain Flame Alchemist, but with more
// spectral analysis and fewer casualties.
//
// macOS only. Needs Microphone and Accessibility permissions; see README.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gen2brain/malgo"
)

const sampleRate = 44100

// Input-event suppression windows: a snap heard right after real keyboard
// or mouse activity is that activity's sound, not a snap.
const (
	keySuppress   = 350 * time.Millisecond
	mouseSuppress = 150 * time.Millisecond
	burstSuppress = 1500 * time.Millisecond // while actively typing
)

func main() {
	key := flag.String("key", "1", "digit key to press on snap (0-9)")
	threshold := flag.Float64("threshold", 0.06, "absolute band-passed peak threshold (0..1)")
	ratio := flag.Float64("ratio", 6, "peak must exceed noise floor by this factor")
	crest := flag.Float64("crest", 3, "min crest factor (peak/RMS) — snaps are impulsive")
	band := flag.Float64("band", 2.5, "min mid/low band energy ratio (rejects keyboard thump)")
	bright := flag.Float64("bright", 1.5, "min mid/high band energy ratio (rejects keyboard click brightness)")
	debounce := flag.Duration("debounce", 200*time.Millisecond, "min interval between triggers")
	gain := flag.Float64("gain", 1, "software input gain multiplier")
	deviceName := flag.String("device", "", "capture device name substring (default: system default)")
	list := flag.Bool("list", false, "list capture devices and exit")
	test := flag.Bool("test", false, "press the key once after 3s and exit (checks Accessibility permission)")
	verbose := flag.Bool("v", false, "print levels for tuning")
	flag.Parse()
	loadConfig()

	keyCode, ok := digitKeyCodes[rune((*key)[0])]
	if len(*key) != 1 || !ok {
		log.Fatalf("-key must be a single digit 0-9, got %q", *key)
	}

	if *test {
		fmt.Printf("focus a text field — pressing %q in 3s...\n", *key)
		time.Sleep(3 * time.Second)
		pressKey(keyCode)
		fmt.Println("done. If no character appeared, grant Accessibility to this app:")
		fmt.Println("System Settings → Privacy & Security → Accessibility")
		return
	}

	snaps := make(chan snapEvent, 4)
	det := &detector{
		threshold: float32(*threshold),
		ratio:     float32(*ratio),
		crest:     float32(*crest),
		band:      float32(*band),
		bright:    float32(*bright),
		gain:      float32(*gain),
		debounce:  *debounce,
		verbose:   *verbose,
		snaps:     snaps,
		window:    make([]float32, 0, sampleRate/4),
		scratch:   make([]float32, 0, sampleRate/10),
	}

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		log.Fatalf("audio context: %v", err)
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	devices, err := ctx.Devices(malgo.Capture)
	if err != nil {
		log.Fatalf("list capture devices: %v", err)
	}
	if *list {
		for _, d := range devices {
			def := ""
			if d.IsDefault != 0 {
				def = "  (default)"
			}
			fmt.Printf("  %s%s\n", d.Name(), def)
		}
		return
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatF32
	cfg.Capture.Channels = 1
	cfg.SampleRate = sampleRate
	cfg.PeriodSizeInMilliseconds = 5

	chosen := "system default"
	if *deviceName != "" {
		found := false
		for i, d := range devices {
			if strings.Contains(strings.ToLower(d.Name()), strings.ToLower(*deviceName)) {
				id := devices[i].ID
				cfg.Capture.DeviceID = id.Pointer()
				chosen = d.Name()
				found = true
				break
			}
		}
		if !found {
			log.Fatalf("no capture device matching %q (use -list)", *deviceName)
		}
	} else {
		for _, d := range devices {
			if d.IsDefault != 0 {
				chosen = d.Name()
				break
			}
		}
	}

	device, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(_, input []byte, frameCount uint32) {
			det.process(input, frameCount)
		},
	})
	if err != nil {
		log.Fatalf("open capture device: %v", err)
	}
	defer device.Uninit()

	if err := device.Start(); err != nil {
		log.Fatalf("start capture: %v", err)
	}

	fmt.Printf("mustang: mic %q → key %q (threshold %.2f, band %.1f, bright %.1f)\n",
		chosen, *key, *threshold, *band, *bright)
	fmt.Println("Ctrl+C to quit. If nothing triggers, run with -v and adjust -threshold.")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	logFile := openLogFile()
	logLine := func(line string) {
		fmt.Print(line)
		if logFile != nil {
			_, _ = logFile.WriteString(line)
		}
	}

	trusted := axTrusted(true) // prompts and registers the app on first run
	logLine(fmt.Sprintf("%s started: mic=%q accessibility=%v\n",
		time.Now().Format("15:04:05.000"), chosen, trusted))
	if !trusted {
		fmt.Println("keypresses will NOT be delivered until Accessibility is granted " +
			"(System Settings → Privacy & Security → Accessibility), then restart the app")
	}

	if trusted {
		startKeyMonitor()
	}

	go func() {
		for ev := range snaps {
			// During an active typing burst clicks arrive in clusters with
			// long acoustic tails — stretch the suppression window.
			window := keySuppress
			if typingBurst() {
				window = burstSuppress
			}
			now := time.Now().Format("15:04:05.000")
			if age := keyDownAge(); age < window.Seconds() {
				logLine(fmt.Sprintf("%s suppressed: real keypress %.0fms ago (window %s, peak=%.3f)\n",
					now, age*1000, window, ev.peak))
			} else if mAge := mouseAge(); mAge < mouseSuppress.Seconds() {
				logLine(fmt.Sprintf("%s suppressed: mouse click %.0fms ago (peak=%.3f)\n",
					now, mAge*1000, ev.peak))
			} else {
				markSelfPress(keyCode)
				pressKey(keyCode)
				flashFlame()
				logLine(fmt.Sprintf("%s snap → %s  (latency=%.0fms buf=%.1fms peak=%.3f mid/low=%.1f mid/high=%.1f)\n",
					now, *key, float64(time.Since(ev.at).Milliseconds()), ev.bufMs,
					ev.peak, ev.midLow, ev.midHigh))
			}
		}
	}()
	go func() {
		<-sig
		fmt.Println("\nbye")
		_ = device.Stop()
		os.Exit(0)
	}()

	// Cocoa main loop: menu-bar item, flame flashes; "Quit Mustang" exits.
	runApp()
}
