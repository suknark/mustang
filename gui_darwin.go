package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore
#include <Cocoa/Cocoa.h>
#include <QuartzCore/QuartzCore.h>

static NSStatusItem *statusItem = nil;
static id activityToken = nil;
static NSWindow *flashWindow = nil;
static NSImage *edgeGlow(NSSize size);
static NSWindow *flashWindowFor(NSRect sf);

// Real-keyboard tracking: any physical keyDown shortly before an acoustic
// trigger means the sound was the keyboard, not a snap. Our own synthetic
// presses are excluded via markSelfPress.
static double lastKeyDown = -1e9;   // CACurrentMediaTime of last real keyDown
static CGKeyCode selfCode = 0xFFFF; // key we synthesize, ignored briefly
static double selfUntil = 0;

// Typing-burst tracking: only real keyDown presses land in the ring —
// modifiers and key releases suppress briefly but must not fake a "typing"
// state (Cmd+Tab would otherwise mute snaps for 1.5s after every window
// switch). Mouse clicks get their own, shorter window.
#define KEYRING 8
static double keyTimes[KEYRING];
static int keyIdx = 0;
static double lastMouse = -1e9;

static void noteKey(double now, bool isTyping) {
	lastKeyDown = now;
	if (!isTyping) return;
	keyTimes[keyIdx] = now;
	keyIdx = (keyIdx + 1) % KEYRING;
}

// typingBurst returns true if ≥3 keys were seen in the last 2 seconds.
static bool typingBurst(void) {
	double now = CACurrentMediaTime();
	int n = 0;
	for (int i = 0; i < KEYRING; i++)
		if (now - keyTimes[i] < 2.0) n++;
	return n >= 3;
}

static void startKeyMonitor(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		// Key presses, releases and modifier keys — all of them click.
		NSEventMask keys = NSEventMaskKeyDown | NSEventMaskKeyUp | NSEventMaskFlagsChanged;
		[NSEvent addGlobalMonitorForEventsMatchingMask:keys
			handler:^(NSEvent *e) {
				double now = CACurrentMediaTime();
				if (e.type != NSEventTypeFlagsChanged &&
				    e.keyCode == selfCode && now < selfUntil) return;
				noteKey(now, e.type == NSEventTypeKeyDown);
			}];
		[NSEvent addLocalMonitorForEventsMatchingMask:keys
			handler:^NSEvent *(NSEvent *e) {
				noteKey(CACurrentMediaTime(), e.type == NSEventTypeKeyDown);
				return e;
			}];
		// Mouse/trackpad clicks are impulsive sounds too.
		NSEventMask clicks = NSEventMaskLeftMouseDown | NSEventMaskLeftMouseUp |
			NSEventMaskRightMouseDown | NSEventMaskRightMouseUp | NSEventMaskOtherMouseDown;
		[NSEvent addGlobalMonitorForEventsMatchingMask:clicks
			handler:^(NSEvent *e) {
				lastMouse = CACurrentMediaTime();
			}];
	});
}

static double keyDownAge(void) {
	return CACurrentMediaTime() - lastKeyDown;
}

static double mouseAge(void) {
	return CACurrentMediaTime() - lastMouse;
}

static void markSelfPress(CGKeyCode code) {
	selfCode = code;
	selfUntil = CACurrentMediaTime() + 0.15;
}

// runApp sets up the menu-bar item and runs the Cocoa main loop (blocks).
static void runApp(void) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];

		statusItem = [[NSStatusBar systemStatusBar]
			statusItemWithLength:NSVariableStatusItemLength];
		statusItem.button.title = @"🔥";

		NSMenu *menu = [[NSMenu alloc] init];
		[menu addItemWithTitle:@"Quit Mustang"
		                action:@selector(terminate:)
		         keyEquivalent:@"q"];
		statusItem.menu = menu;

		// Keep the process out of App Nap: a napped agent gets its queues
		// throttled and reacts to snaps with visible sporadic lag.
		activityToken = [[NSProcessInfo processInfo]
			beginActivityWithOptions:(NSActivityUserInitiated | NSActivityLatencyCritical)
			              reason:@"real-time snap detection"];

		// Pre-create the flash window (and its glow image) so the first
		// flash is as instant as the rest.
		dispatch_async(dispatch_get_main_queue(), ^{
			NSScreen *s = [NSScreen mainScreen];
			if (s != nil) flashWindowFor(s.frame);
		});

		[NSApp run];
	}
}

// edgeGlow renders a fiery vignette: two gradient layers burning inward
// from every screen edge — a wide dim red glow and a narrow hot rim.
static NSImage *glowCache = nil;
static NSSize glowCacheSize = {0, 0};

static void drawEdges(NSGradient *g, CGFloat w, CGFloat h, CGFloat depth) {
	[g drawInRect:NSMakeRect(0, 0, w, depth) angle:90];          // bottom
	[g drawInRect:NSMakeRect(0, h - depth, w, depth) angle:270]; // top
	[g drawInRect:NSMakeRect(0, 0, depth, h) angle:0];           // left
	[g drawInRect:NSMakeRect(w - depth, 0, depth, h) angle:180]; // right
}

static NSImage *edgeGlow(NSSize size) {
	if (glowCache != nil && NSEqualSizes(size, glowCacheSize)) return glowCache;
	NSImage *img = [[NSImage alloc] initWithSize:size];
	[img lockFocus];
	CGFloat w = size.width, h = size.height;

	NSColor *ember = [NSColor colorWithCalibratedRed:0.85 green:0.12 blue:0.02 alpha:0.50];
	NSColor *flame = [NSColor colorWithCalibratedRed:1.00 green:0.45 blue:0.08 alpha:0.90];
	NSColor *clear = [NSColor colorWithCalibratedRed:1.00 green:0.30 blue:0.00 alpha:0.00];

	// Full-screen fiery wash plus edge glows deep enough to meet in the
	// middle — the whole screen burns, hottest at the rim.
	[[NSColor colorWithCalibratedRed:0.90 green:0.18 blue:0.03 alpha:0.22] setFill];
	NSRectFillUsingOperation(NSMakeRect(0, 0, w, h), NSCompositingOperationSourceOver);
	CGFloat m = w < h ? w : h;
	drawEdges([[NSGradient alloc] initWithStartingColor:ember endingColor:clear], w, h, m * 0.70);
	drawEdges([[NSGradient alloc] initWithStartingColor:flame endingColor:clear], w, h, m * 0.18);

	[img unlockFocus];
	glowCache = img;
	glowCacheSize = size;
	return img;
}

// flashWindowFor returns the persistent full-screen flash window, creating
// it on first use — reusing one window avoids window-server allocation on
// every snap.
static NSWindow *flashWindowFor(NSRect sf) {
	if (flashWindow != nil && NSEqualRects(flashWindow.frame, sf)) return flashWindow;
	NSWindow *w = [[NSWindow alloc] initWithContentRect:sf
		styleMask:NSWindowStyleMaskBorderless
		  backing:NSBackingStoreBuffered
		    defer:NO];
	w.opaque = NO;
	w.backgroundColor = [NSColor clearColor];
	w.level = NSStatusWindowLevel;
	w.ignoresMouseEvents = YES;
	w.hasShadow = NO;
	w.releasedWhenClosed = NO;
	w.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
		NSWindowCollectionBehaviorStationary;

	NSImageView *iv = [NSImageView imageViewWithImage:edgeGlow(sf.size)];
	iv.frame = ((NSView *)w.contentView).bounds;
	iv.imageScaling = NSImageScaleAxesIndependently;
	[w.contentView addSubview:iv];

	// Keep the window permanently ordered in at alpha 0: flashing is then
	// just an alpha change — no window-server add/remove on the hot path.
	w.alphaValue = 0.0;
	[w orderFrontRegardless];

	flashWindow = w;
	return w;
}

// flashFlame makes the screen edges flare up and burn out.
// Safe to call from any thread.
static void flashFlame(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSScreen *screen = [NSScreen mainScreen];
		if (screen == nil) return;
		NSWindow *w = flashWindowFor(screen.frame);

		// Appear instantly (cancel any running fade), then burn out.
		[NSAnimationContext beginGrouping];
		[NSAnimationContext.currentContext setDuration:0.0];
		w.animator.alphaValue = 1.0;
		[NSAnimationContext endGrouping];
		[NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
			ctx.duration = 0.6;
			ctx.timingFunction = [CAMediaTimingFunction functionWithName:
				kCAMediaTimingFunctionEaseOut];
			w.animator.alphaValue = 0.0;
		} completionHandler:nil];
	});
}
*/
import "C"
import "runtime"

func init() {
	// Cocoa requires its event loop on the process's first thread.
	runtime.LockOSThread()
}

// runApp blocks running the Cocoa main loop (menu-bar item, flash windows).
func runApp() {
	C.runApp()
}

// flashFlame briefly shows the flame overlay. Callable from any goroutine.
func flashFlame() {
	C.flashFlame()
}

// startKeyMonitor begins tracking real keyboard presses (needs Accessibility).
func startKeyMonitor() {
	C.startKeyMonitor()
}

// keyDownAge returns seconds since the last real (non-synthetic) keyDown.
func keyDownAge() float64 {
	return float64(C.keyDownAge())
}

// typingBurst reports whether the user is actively typing (≥3 keys in 2s).
func typingBurst() bool {
	return bool(C.typingBurst())
}

// mouseAge returns seconds since the last mouse/trackpad click.
func mouseAge() float64 {
	return float64(C.mouseAge())
}

// markSelfPress excludes our own upcoming synthetic press from the monitor.
func markSelfPress(code uint16) {
	C.markSelfPress(C.CGKeyCode(code))
}
