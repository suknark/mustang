package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// snapEvent carries the measurements of a confirmed snap.
type snapEvent struct {
	peak    float32   // band-passed peak of the trigger buffer
	midLow  float32   // mid-band / low-band energy ratio over the analysis window
	midHigh float32   // mid-band / high-band energy ratio
	at      time.Time // when the candidate transient was detected
	bufMs   float64   // actual audio buffer duration reported by the OS
}

// Spectral bands, chosen from published measurements of finger snaps:
// snap energy sits in ~1.5–4.5 kHz and falls off both below and above,
// while a keyboard press adds a <800 Hz bottom-out thud and a bright
// plastic click above 6 kHz.
var (
	lowFreqs  = []float64{300, 500, 800}
	midFreqs  = []float64{2000, 2800, 3600, 4400}
	highFreqs = []float64{6000, 7500, 9000}
)

// windowSamples is the analysis window around a candidate: ~25ms. Long
// enough for the Goertzel bands (300 Hz ≈ 7 cycles), short enough that the
// press fires while the snap still hangs in the air; catching a keyboard's
// delayed bottom-out thud is the input-event filter's job.
const windowSamples = sampleRate / 40

// Band-pass ~1.6–4.5 kHz as a first-order high-pass + low-pass cascade.
// HP: y[n] = a*(y[n-1] + x[n] - x[n-1]);  LP: z[n] = z[n-1] + b*(y[n] - z[n-1])
const (
	hpAlpha = 0.81  // ≈1.6 kHz at 44.1 kHz
	lpBeta  = 0.473 // ≈4.5 kHz at 44.1 kHz
)

// detector spots finger snaps in the mic stream in two stages:
//
//  1. Candidate: a loud impulsive transient in the snap band only — the
//     signal is band-passed to ~1.6–4.5 kHz (where snap energy lives)
//     before peak/floor/crest checks, so keyboard thumps (<800 Hz) and
//     bright switch clicks (>6 kHz) are attenuated before detection.
//  2. Spectral confirmation: samples are collected for ~25ms around the
//     transient, then band energies are measured with Goertzel bins.
//     A snap must dominate in the mid band (2–4.4 kHz) over both the low
//     band (keyboard thud) and the high band (keyboard click brightness).
type detector struct {
	threshold float32
	ratio     float32
	crest     float32
	band      float32
	bright    float32
	gain      float32
	debounce  time.Duration
	verbose   bool
	snaps     chan<- snapEvent

	hpPrevIn    float32 // one-pole high-pass state (band-pass lower edge)
	hpPrevOut   float32
	lpBand      float32 // one-pole low-pass state (band-pass upper edge)
	noiseFloor  float32
	prevPeak    float32
	lastTrigger time.Time
	lastLog     time.Time
	logMaxPeak  float32 // loudest band peak since last verbose line

	collecting bool      // a candidate fired; gathering the analysis window
	pendPeak   float32   // band peak of the candidate buffer
	pendAt     time.Time // when the candidate was spotted
	window     []float32 // raw samples of candidate + following buffers
	scratch    []float32 // raw samples of the current buffer

	bufMs        float64
	loggedFrames bool
}

func (d *detector) process(input []byte, frameCount uint32) {
	if !d.loggedFrames {
		d.loggedFrames = true
		d.bufMs = float64(frameCount) * 1000 / sampleRate
		fmt.Printf("  audio buffer: %d frames (%.1fms)\n", frameCount, d.bufMs)
	}
	var peak, sumSq float32
	d.scratch = d.scratch[:0]
	for i := uint32(0); i < frameCount; i++ {
		bits := binary.LittleEndian.Uint32(input[i*4:])
		x := math.Float32frombits(bits) * d.gain
		d.scratch = append(d.scratch, x)
		y := hpAlpha * (d.hpPrevOut + x - d.hpPrevIn)
		d.hpPrevIn, d.hpPrevOut = x, y
		d.lpBand += lpBeta * (y - d.lpBand)
		v := d.lpBand
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
		sumSq += v * v
	}
	rms := float32(math.Sqrt(float64(sumSq / float32(frameCount))))

	// Slow EMA of RMS tracks ambient noise; snaps barely move it.
	if d.noiseFloor == 0 {
		d.noiseFloor = rms
	}
	d.noiseFloor += 0.02 * (rms - d.noiseFloor)
	floor := d.noiseFloor
	if floor < 0.001 {
		floor = 0.001
	}

	if peak > d.logMaxPeak {
		d.logMaxPeak = peak
	}
	if d.verbose && time.Since(d.lastLog) > 200*time.Millisecond {
		d.lastLog = time.Now()
		fmt.Printf("  maxpeak=%.3f rms=%.3f floor=%.3f\n", d.logMaxPeak, rms, floor)
		d.logMaxPeak = 0
	}

	prevQuiet := d.prevPeak < peak*0.5
	d.prevPeak = peak

	// Collecting the analysis window for a pending candidate.
	if d.collecting {
		d.window = append(d.window, d.scratch...)
		if len(d.window) >= windowSamples {
			d.collecting = false
			d.classify()
		}
		return
	}

	if peak >= d.threshold &&
		peak >= floor*d.ratio &&
		peak >= rms*d.crest &&
		prevQuiet &&
		time.Since(d.lastTrigger) >= d.debounce {
		d.pendPeak = peak
		d.pendAt = time.Now()
		d.window = append(d.window[:0], d.scratch...)
		if len(d.window) >= windowSamples {
			d.classify()
		} else {
			d.collecting = true
		}
	} else if d.verbose && peak >= d.threshold && time.Since(d.lastTrigger) >= d.debounce {
		fmt.Printf("  candidate rejected: peak=%.3f crest=%.1f prevQuiet=%v\n",
			peak, peak/(rms+1e-6), prevQuiet)
	}
}

// classify measures band energies over the collected window and decides
// whether the transient was a snap.
func (d *detector) classify() {
	low := bandEnergy(d.window, lowFreqs)
	mid := bandEnergy(d.window, midFreqs)
	high := bandEnergy(d.window, highFreqs)
	midLow := mid / (low + 1e-12)
	midHigh := mid / (high + 1e-12)

	if float32(midLow) >= d.band && float32(midHigh) >= d.bright {
		d.lastTrigger = time.Now()
		select {
		case d.snaps <- snapEvent{peak: d.pendPeak, midLow: float32(midLow),
			midHigh: float32(midHigh), at: d.pendAt, bufMs: d.bufMs}:
		default:
		}
	} else if d.verbose {
		fmt.Printf("  rejected: peak=%.3f mid/low=%.1f (need %.1f) mid/high=%.1f (need %.1f)\n",
			d.pendPeak, midLow, d.band, midHigh, d.bright)
	}
}

// bandEnergy returns the mean Goertzel power of the given frequencies.
func bandEnergy(samples []float32, freqs []float64) float64 {
	var sum float64
	for _, f := range freqs {
		sum += goertzelPower(samples, f)
	}
	return sum / float64(len(freqs))
}

// goertzelPower computes the DFT power at a single frequency.
func goertzelPower(samples []float32, freq float64) float64 {
	coeff := 2 * math.Cos(2*math.Pi*freq/sampleRate)
	var s1, s2 float64
	for _, x := range samples {
		s := float64(x) + coeff*s1 - s2
		s2, s1 = s1, s
	}
	power := s1*s1 + s2*s2 - coeff*s1*s2
	return power / float64(len(samples)) // normalize by window length
}
