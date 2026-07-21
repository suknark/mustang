package main

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

// axTrusted reports whether the process may post keyboard events; with
// prompt=true macOS shows the standard dialog and adds the app to the
// Accessibility list in System Settings.
static bool axTrusted(bool prompt) {
	CFStringRef keys[] = { kAXTrustedCheckOptionPrompt };
	CFBooleanRef vals[] = { prompt ? kCFBooleanTrue : kCFBooleanFalse };
	CFDictionaryRef opts = CFDictionaryCreate(NULL,
		(const void **)keys, (const void **)vals, 1,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	bool t = AXIsProcessTrustedWithOptions(opts);
	CFRelease(opts);
	return t;
}

static void pressKey(CGKeyCode code) {
	CGEventRef down = CGEventCreateKeyboardEvent(NULL, code, true);
	CGEventRef up   = CGEventCreateKeyboardEvent(NULL, code, false);
	CGEventPost(kCGHIDEventTap, down);
	CGEventPost(kCGHIDEventTap, up);
	CFRelease(down);
	CFRelease(up);
}
*/
import "C"

// macOS virtual key codes (ANSI layout) for digit keys.
var digitKeyCodes = map[rune]uint16{
	'1': 18, '2': 19, '3': 20, '4': 21, '5': 23,
	'6': 22, '7': 26, '8': 28, '9': 25, '0': 29,
}

func pressKey(code uint16) {
	C.pressKey(C.CGKeyCode(code))
}

// axTrusted reports whether keyboard events will be delivered; when prompt
// is true macOS shows the grant dialog and lists the app in Accessibility.
func axTrusted(prompt bool) bool {
	return bool(C.axTrusted(C.bool(prompt)))
}
