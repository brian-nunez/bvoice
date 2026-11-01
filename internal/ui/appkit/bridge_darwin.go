//go:build darwin

package appkit

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit

#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

// Go callbacks exported from appkit_darwin.go. Declared here rather than via _cgo_export.h,
// which cgo only generates after it has compiled this preamble.
extern void goUIReady(void);
extern void goUIAction(int kind, char *ckey, char *cval);

// Must match the constants in appkit_darwin.go.
enum { BVSetStatus = 1, BVSetHistory = 2, BVSetControls = 3, BVSetNotice = 4 };
enum { BVToggle = 1, BVSetting = 2, BVSearch = 3, BVCopy = 4, BVExport = 5, BVClear = 6, BVSync = 7, BVQuit = 8 };

static void bv_send(int kind, NSString *key, NSString *val) {
	goUIAction(kind, (char *)(key ?: @"").UTF8String, (char *)(val ?: @"").UTF8String);
}

static NSString *bv_str(id v) {
	if ([v isKindOfClass:[NSString class]]) return v;
	if ([v respondsToSelector:@selector(stringValue)]) return [v stringValue];
	return @"";
}

static NSString *bv_hotkey_label(NSString *hk) {
	if (hk.length == 0) return @"the hotkey";
	if ([hk isEqualToString:@"fn"]) return @"Fn";
	return [[hk stringByReplacingOccurrencesOfString:@"_" withString:@" "] capitalizedString];
}

static NSTextField *bv_label(NSString *text, CGFloat size, BOOL bold, BOOL secondary) {
	NSTextField *l = [NSTextField labelWithString:text ?: @""];
	l.font = bold ? [NSFont boldSystemFontOfSize:size] : [NSFont systemFontOfSize:size];
	if (secondary) l.textColor = [NSColor secondaryLabelColor];
	l.lineBreakMode = NSLineBreakByTruncatingTail;
	return l;
}

// BVPanel hides on Esc and can take key focus (for search and settings) without activating the app.

@interface BVPanel : NSPanel
@end
@implementation BVPanel
- (BOOL)canBecomeKeyWindow { return YES; }
- (void)cancelOperation:(id)sender { [self orderOut:nil]; }
@end

@interface BVFlipped : NSView
@end
@implementation BVFlipped
- (BOOL)isFlipped { return YES; }
@end

@interface BVController : NSObject <NSTableViewDataSource, NSTableViewDelegate, NSWindowDelegate>
@property(strong) NSStatusItem *statusItem;
@property(strong) BVPanel *panel;
@property(strong) NSStackView *root;
@property(strong) NSTextField *statusLabel;
@property(strong) NSTextField *detailLabel;
@property(strong) NSTextField *messageLabel;
@property(strong) NSButton *toggleButton;
@property(strong) NSBox *noticeBox;
@property(strong) NSTextField *noticeLabel;
@property(strong) NSSearchField *search;
@property(strong) NSTableView *table;
@property(strong) NSStackView *settingsStack;
@property(strong) NSArray *history;
@property(strong) NSDictionary *status;
@property(strong) NSTimer *timer;
@property BOOL positioned;
@end

@implementation BVController

- (NSImage *)symbol:(NSString *)name {
	NSImage *img = [NSImage imageWithSystemSymbolName:name accessibilityDescription:@"bvoice"];
	img.template = YES;
	return img;
}

- (void)addFull:(NSView *)v to:(NSStackView *)stack inset:(CGFloat)inset {
	[stack addArrangedSubview:v];
	[v.widthAnchor constraintEqualToAnchor:stack.widthAnchor constant:-2 * inset].active = YES;
}

- (void)setupShowing:(BOOL)show {
	[self setupMainMenu];

	// menu bar icon
	self.statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
	NSStatusBarButton *b = self.statusItem.button;
	b.image = [self symbol:@"waveform"];
	b.toolTip = @"bvoice";
	b.target = self;
	b.action = @selector(statusClicked:);
	[b sendActionOn:NSEventMaskLeftMouseUp | NSEventMaskRightMouseUp];

	// floating panel
	self.panel = [[BVPanel alloc] initWithContentRect:NSMakeRect(0, 0, 380, 580)
	                                        styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
	                                                  NSWindowStyleMaskResizable | NSWindowStyleMaskUtilityWindow |
	                                                  NSWindowStyleMaskNonactivatingPanel
	                                          backing:NSBackingStoreBuffered
	                                            defer:NO];
	self.panel.title = @"bvoice";
	self.panel.releasedWhenClosed = NO;
	self.panel.hidesOnDeactivate = NO;
	self.panel.becomesKeyOnlyIfNeeded = YES;
	self.panel.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary;
	self.panel.minSize = NSMakeSize(320, 440);
	self.panel.delegate = self;
	[self setOnTop:YES];

	NSStackView *root = [NSStackView stackViewWithViews:@[]];
	root.orientation = NSUserInterfaceLayoutOrientationVertical;
	root.alignment = NSLayoutAttributeLeading;
	root.spacing = 8;
	root.edgeInsets = NSEdgeInsetsMake(12, 12, 12, 12);
	root.distribution = NSStackViewDistributionFill; // the tabs take the spare height
	root.translatesAutoresizingMaskIntoConstraints = NO;
	self.root = root;
	NSView *content = self.panel.contentView;
	[content addSubview:root];
	[NSLayoutConstraint activateConstraints:@[
		[root.leadingAnchor constraintEqualToAnchor:content.leadingAnchor],
		[root.trailingAnchor constraintEqualToAnchor:content.trailingAnchor],
		[root.topAnchor constraintEqualToAnchor:content.topAnchor],
		[root.bottomAnchor constraintEqualToAnchor:content.bottomAnchor],
	]];

	// header
	self.statusLabel = bv_label(@"Starting…", 14, YES, NO);
	self.detailLabel = bv_label(@"", 11, NO, YES);
	self.messageLabel = bv_label(@"", 11, NO, YES);
	self.messageLabel.hidden = YES;
	[self addFull:self.statusLabel to:root inset:12];
	[self addFull:self.detailLabel to:root inset:12];
	[self addFull:self.messageLabel to:root inset:12];

	self.toggleButton = [NSButton buttonWithTitle:@"Start Dictation" target:self action:@selector(toggleClicked:)];
	NSButton *quit = [NSButton buttonWithTitle:@"Quit" target:self action:@selector(quitClicked:)];
	NSStackView *buttons = [NSStackView stackViewWithViews:@[ self.toggleButton, quit ]];
	buttons.spacing = 8;
	[root addArrangedSubview:buttons];

	// notice shown when launch flags differ from the saved settings
	self.noticeBox = [[NSBox alloc] init];
	self.noticeBox.boxType = NSBoxCustom;
	self.noticeBox.cornerRadius = 6;
	self.noticeBox.borderColor = [NSColor systemYellowColor];
	self.noticeBox.fillColor = [[NSColor systemYellowColor] colorWithAlphaComponent:0.12];
	self.noticeBox.contentViewMargins = NSMakeSize(8, 8);
	self.noticeLabel = [NSTextField wrappingLabelWithString:
	    @"You started bvoice with different options than your saved settings. Do you want to sync them?"];
	self.noticeLabel.font = [NSFont systemFontOfSize:11];
	NSButton *sync = [NSButton buttonWithTitle:@"Sync" target:self action:@selector(syncClicked:)];
	NSStackView *noticeRow = [NSStackView stackViewWithViews:@[ self.noticeLabel, sync ]];
	noticeRow.alignment = NSLayoutAttributeCenterY;
	[self.noticeLabel setContentCompressionResistancePriority:NSLayoutPriorityDefaultLow
	                                           forOrientation:NSLayoutConstraintOrientationHorizontal];
	self.noticeBox.contentView = noticeRow;
	self.noticeBox.hidden = YES;
	[self addFull:self.noticeBox to:root inset:12];

	// tabs
	NSTabView *tabs = [[NSTabView alloc] init];
	NSTabViewItem *h = [[NSTabViewItem alloc] initWithIdentifier:@"history"];
	h.label = @"History";
	h.view = [self buildHistoryView];
	NSTabViewItem *s = [[NSTabViewItem alloc] initWithIdentifier:@"settings"];
	s.label = @"Settings";
	s.view = [self buildSettingsView];
	[tabs addTabViewItem:h];
	[tabs addTabViewItem:s];
	[tabs setContentHuggingPriority:1 forOrientation:NSLayoutConstraintOrientationVertical];
	[self addFull:tabs to:root inset:12];

	if (show) [self showPanel];
}

- (void)setupMainMenu {
	// An accessory app has no visible menu bar, but text fields still need Edit shortcuts (⌘C/⌘V/⌘A…).
	NSMenu *main = [[NSMenu alloc] init];
	NSMenuItem *editItem = [[NSMenuItem alloc] init];
	NSMenu *edit = [[NSMenu alloc] initWithTitle:@"Edit"];
	[edit addItemWithTitle:@"Undo" action:@selector(undo:) keyEquivalent:@"z"];
	[edit addItemWithTitle:@"Redo" action:@selector(redo:) keyEquivalent:@"Z"];
	[edit addItem:[NSMenuItem separatorItem]];
	[edit addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"];
	[edit addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
	[edit addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
	[edit addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
	editItem.submenu = edit;
	[main addItem:editItem];
	NSApp.mainMenu = main;
}

- (NSView *)buildHistoryView {
	NSStackView *v = [NSStackView stackViewWithViews:@[]];
	v.orientation = NSUserInterfaceLayoutOrientationVertical;
	v.alignment = NSLayoutAttributeLeading;
	v.spacing = 8;
	v.edgeInsets = NSEdgeInsetsMake(8, 8, 8, 8);
	v.distribution = NSStackViewDistributionFill; // the list takes the spare height

	self.search = [[NSSearchField alloc] init];
	self.search.placeholderString = @"Search transcriptions";
	self.search.target = self;
	self.search.action = @selector(searchChanged:);
	[self addFull:self.search to:v inset:8];

	self.table = [[NSTableView alloc] init];
	NSTableColumn *col = [[NSTableColumn alloc] initWithIdentifier:@"entry"];
	col.resizingMask = NSTableColumnAutoresizingMask;
	[self.table addTableColumn:col];
	self.table.headerView = nil;
	self.table.rowHeight = 54;
	self.table.columnAutoresizingStyle = NSTableViewUniformColumnAutoresizingStyle;
	self.table.dataSource = self;
	self.table.delegate = self;
	self.table.target = self;
	self.table.action = @selector(rowClicked:);
	self.table.toolTip = @"Click a transcription to copy it";
	NSScrollView *scroll = [[NSScrollView alloc] init];
	scroll.documentView = self.table;
	scroll.hasVerticalScroller = YES;
	scroll.borderType = NSBezelBorder;
	[scroll setContentHuggingPriority:1 forOrientation:NSLayoutConstraintOrientationVertical];
	[self addFull:scroll to:v inset:8];

	NSButton *exp = [NSButton buttonWithTitle:@"Export…" target:self action:@selector(exportClicked:)];
	NSButton *clr = [NSButton buttonWithTitle:@"Clear History…" target:self action:@selector(clearClicked:)];
	NSStackView *row = [NSStackView stackViewWithViews:@[ exp, clr ]];
	[v addArrangedSubview:row];
	return v;
}

- (NSInteger)numberOfRowsInTableView:(NSTableView *)tv {
	return self.history.count;
}

- (NSView *)tableView:(NSTableView *)tv viewForTableColumn:(NSTableColumn *)col row:(NSInteger)row {
	NSTableCellView *cell = [tv makeViewWithIdentifier:@"entryCell" owner:self];
	NSTextField *meta, *text;
	if (!cell) {
		cell = [[NSTableCellView alloc] init];
		cell.identifier = @"entryCell";
		meta = bv_label(@"", 10, NO, YES);
		meta.tag = 1;
		text = bv_label(@"", 12, NO, NO);
		text.tag = 2;
		text.maximumNumberOfLines = 2;
		text.lineBreakMode = NSLineBreakByTruncatingTail;
		text.cell.wraps = YES;
		NSStackView *st = [NSStackView stackViewWithViews:@[ meta, text ]];
		st.orientation = NSUserInterfaceLayoutOrientationVertical;
		st.alignment = NSLayoutAttributeLeading;
		st.spacing = 2;
		st.translatesAutoresizingMaskIntoConstraints = NO;
		[cell addSubview:st];
		[NSLayoutConstraint activateConstraints:@[
			[st.leadingAnchor constraintEqualToAnchor:cell.leadingAnchor constant:4],
			[st.trailingAnchor constraintEqualToAnchor:cell.trailingAnchor constant:-4],
			[st.centerYAnchor constraintEqualToAnchor:cell.centerYAnchor],
			[text.widthAnchor constraintEqualToAnchor:st.widthAnchor],
		]];
	} else {
		meta = [cell viewWithTag:1];
		text = [cell viewWithTag:2];
	}
	NSDictionary *e = self.history[row];
	meta.stringValue = [NSString stringWithFormat:@"%@ · %@", bv_str(e[@"time"]), bv_str(e[@"meta"])];
	text.stringValue = bv_str(e[@"preview"]);
	return cell;
}

- (void)rowClicked:(id)sender {
	NSInteger row = self.table.clickedRow;
	if (row < 0 || row >= (NSInteger)self.history.count) return;
	bv_send(BVCopy, nil, bv_str(self.history[row][@"id"]));
}

- (void)searchChanged:(id)sender {
	bv_send(BVSearch, nil, self.search.stringValue);
}

- (void)exportClicked:(id)sender {
	[NSApp activateIgnoringOtherApps:YES];
	NSSavePanel *sp = [NSSavePanel savePanel];
	sp.nameFieldStringValue = @"bvoice-history.md";
	[sp beginSheetModalForWindow:self.panel completionHandler:^(NSModalResponse r) {
		if (r == NSModalResponseOK && sp.URL) bv_send(BVExport, nil, sp.URL.path);
	}];
}

- (void)clearClicked:(id)sender {
	[NSApp activateIgnoringOtherApps:YES];
	NSAlert *a = [[NSAlert alloc] init];
	a.messageText = @"Clear all history?";
	a.informativeText = @"This deletes every saved transcription. It can't be undone.";
	[a addButtonWithTitle:@"Clear"];
	[a addButtonWithTitle:@"Cancel"];
	a.buttons[0].hasDestructiveAction = YES;
	[a beginSheetModalForWindow:self.panel completionHandler:^(NSModalResponse r) {
		if (r == NSAlertFirstButtonReturn) bv_send(BVClear, nil, nil);
	}];
}

- (NSView *)buildSettingsView {
	NSScrollView *scroll = [[NSScrollView alloc] init];
	scroll.hasVerticalScroller = YES;
	scroll.drawsBackground = NO;
	BVFlipped *doc = [[BVFlipped alloc] init];
	doc.translatesAutoresizingMaskIntoConstraints = NO;
	scroll.documentView = doc;

	self.settingsStack = [NSStackView stackViewWithViews:@[]];
	self.settingsStack.orientation = NSUserInterfaceLayoutOrientationVertical;
	self.settingsStack.alignment = NSLayoutAttributeLeading;
	self.settingsStack.spacing = 10;
	self.settingsStack.edgeInsets = NSEdgeInsetsMake(10, 10, 10, 10);
	self.settingsStack.translatesAutoresizingMaskIntoConstraints = NO;
	[doc addSubview:self.settingsStack];
	[NSLayoutConstraint activateConstraints:@[
		[self.settingsStack.leadingAnchor constraintEqualToAnchor:doc.leadingAnchor],
		[self.settingsStack.trailingAnchor constraintEqualToAnchor:doc.trailingAnchor],
		[self.settingsStack.topAnchor constraintEqualToAnchor:doc.topAnchor],
		[self.settingsStack.bottomAnchor constraintEqualToAnchor:doc.bottomAnchor],
		[doc.leadingAnchor constraintEqualToAnchor:scroll.contentView.leadingAnchor],
		[doc.topAnchor constraintEqualToAnchor:scroll.contentView.topAnchor],
		[doc.widthAnchor constraintEqualToAnchor:scroll.contentView.widthAnchor],
	]];
	return scroll;
}

- (void)setControls:(NSArray *)controls {
	for (NSView *v in [self.settingsStack.arrangedSubviews copy]) {
		[self.settingsStack removeArrangedSubview:v];
		[v removeFromSuperview];
	}
	for (NSDictionary *c in controls) {
		NSString *key = bv_str(c[@"key"]), *kind = bv_str(c[@"kind"]), *label = bv_str(c[@"label"]);
		NSString *value = bv_str(c[@"value"]), *help = bv_str(c[@"help"]);
		NSView *row = nil;

		if ([kind isEqualToString:@"bool"]) {
			NSButton *cb = [NSButton checkboxWithTitle:label target:self action:@selector(checkChanged:)];
			cb.state = [value isEqualToString:@"true"] ? NSControlStateValueOn : NSControlStateValueOff;
			cb.identifier = key;
			cb.toolTip = help;
			row = cb;
		} else if ([kind isEqualToString:@"float"]) {
			NSSlider *sl = [NSSlider sliderWithValue:value.doubleValue
			                                minValue:[c[@"min"] doubleValue]
			                                maxValue:[c[@"max"] doubleValue]
			                                  target:self
			                                  action:@selector(sliderChanged:)];
			sl.continuous = NO;
			sl.identifier = key;
			sl.toolTip = help;
			[sl.widthAnchor constraintEqualToConstant:150].active = YES;
			row = [NSStackView stackViewWithViews:@[ bv_label(label, 12, NO, NO), sl ]];
		} else if ([kind isEqualToString:@"int"]) {
			NSTextField *f = [NSTextField textFieldWithString:value];
			f.identifier = key;
			f.toolTip = help;
			f.target = self;
			f.action = @selector(fieldChanged:);
			f.cell.sendsActionOnEndEditing = YES;
			[f.widthAnchor constraintEqualToConstant:70].active = YES;
			row = [NSStackView stackViewWithViews:@[ bv_label(label, 12, NO, NO), f ]];
		} else if ([kind isEqualToString:@"choice"]) {
			NSPopUpButton *p = [[NSPopUpButton alloc] init];
			for (NSDictionary *ch in c[@"choices"]) {
				[p addItemWithTitle:bv_str(ch[@"label"])];
				p.lastItem.representedObject = bv_str(ch[@"value"]);
			}
			for (NSMenuItem *it in p.itemArray) {
				if ([it.representedObject isEqualToString:value]) [p selectItem:it];
			}
			p.identifier = key;
			p.toolTip = help;
			p.target = self;
			p.action = @selector(popupChanged:);
			row = [NSStackView stackViewWithViews:@[ bv_label(label, 12, NO, NO), p ]];
		} else { // text
			NSTextField *f = [NSTextField textFieldWithString:value];
			f.identifier = key;
			f.toolTip = help;
			f.placeholderString = @"e.g. Kubernetes, PostgreSQL, bvoice";
			f.usesSingleLineMode = NO;
			f.cell.wraps = YES;
			f.cell.scrollable = NO;
			f.cell.sendsActionOnEndEditing = YES;
			f.target = self;
			f.action = @selector(fieldChanged:);
			[f.heightAnchor constraintEqualToConstant:54].active = YES;
			NSStackView *st = [NSStackView stackViewWithViews:@[ bv_label(label, 12, NO, NO), f ]];
			st.orientation = NSUserInterfaceLayoutOrientationVertical;
			st.alignment = NSLayoutAttributeLeading;
			[f.widthAnchor constraintEqualToAnchor:st.widthAnchor].active = YES;
			row = st;
		}
		[self addFull:row to:self.settingsStack inset:10];
	}
}

- (void)checkChanged:(NSButton *)sender {
	bv_send(BVSetting, sender.identifier, sender.state == NSControlStateValueOn ? @"true" : @"false");
}
- (void)sliderChanged:(NSSlider *)sender {
	bv_send(BVSetting, sender.identifier, [NSString stringWithFormat:@"%.2f", sender.doubleValue]);
}
- (void)fieldChanged:(NSTextField *)sender {
	bv_send(BVSetting, sender.identifier, sender.stringValue);
}
- (void)popupChanged:(NSPopUpButton *)sender {
	bv_send(BVSetting, sender.identifier, sender.selectedItem.representedObject ?: @"");
}

- (void)setStatusDict:(NSDictionary *)st {
	self.status = st;
	NSString *phase = bv_str(st[@"phase"]);
	NSStatusBarButton *b = self.statusItem.button;
	if ([phase isEqualToString:@"listening"]) {
		b.image = [self symbol:@"mic.fill"];
		b.contentTintColor = [NSColor systemRedColor];
		self.toggleButton.title = @"Stop Dictation";
		self.toggleButton.enabled = YES;
		if (!self.timer) {
			self.timer = [NSTimer scheduledTimerWithTimeInterval:0.5 target:self selector:@selector(tick:) userInfo:nil repeats:YES];
		}
	} else {
		[self.timer invalidate];
		self.timer = nil;
		b.contentTintColor = nil;
		if ([phase isEqualToString:@"finishing"]) {
			b.image = [self symbol:@"ellipsis.circle"];
			self.toggleButton.title = @"Finishing…";
			self.toggleButton.enabled = NO;
		} else {
			b.image = [self symbol:@"waveform"];
			self.toggleButton.title = @"Start Dictation";
			self.toggleButton.enabled = YES;
		}
	}
	self.detailLabel.stringValue = [NSString stringWithFormat:@"Model %@ · %@", bv_str(st[@"model"]), bv_str(st[@"version"])];
	NSString *msg = bv_str(st[@"message"]);
	self.messageLabel.stringValue = msg;
	self.messageLabel.hidden = msg.length == 0;
	[self tick:nil];
}

- (void)tick:(NSTimer *)t {
	NSDictionary *st = self.status;
	NSString *phase = bv_str(st[@"phase"]);
	NSString *hk = bv_hotkey_label(bv_str(st[@"hotkey"]));
	if ([phase isEqualToString:@"listening"]) {
		long long started = [st[@"startedAtMs"] longLongValue];
		long long secs = (long long)([[NSDate date] timeIntervalSince1970] * 1000 - started) / 1000;
		if (secs < 0) secs = 0;
		self.statusLabel.stringValue = [NSString stringWithFormat:@"● Listening %lld:%02lld — tap %@ to stop", secs / 60, secs % 60, hk];
		self.statusLabel.textColor = [NSColor systemRedColor];
	} else if ([phase isEqualToString:@"finishing"]) {
		self.statusLabel.stringValue = @"Finishing transcription…";
		self.statusLabel.textColor = [NSColor labelColor];
	} else {
		self.statusLabel.stringValue = [NSString stringWithFormat:@"Ready — tap %@ to dictate", hk];
		self.statusLabel.textColor = [NSColor labelColor];
	}
}

- (void)setNotice:(NSArray *)changes {
	self.noticeBox.hidden = changes.count == 0;
	NSMutableArray *lines = [NSMutableArray array];
	for (NSDictionary *c in changes) {
		[lines addObject:[NSString stringWithFormat:@"%@: saved %@, this run %@", bv_str(c[@"label"]), bv_str(c[@"saved"]), bv_str(c[@"flag"])]];
	}
	self.noticeLabel.toolTip = [lines componentsJoinedByString:@"\n"];
}

- (void)setHistoryArray:(NSArray *)items {
	self.history = items;
	[self.table reloadData];
}

- (void)setOnTop:(BOOL)on {
	self.panel.floatingPanel = on;
	self.panel.level = on ? NSFloatingWindowLevel : NSNormalWindowLevel;
}

- (void)statusClicked:(id)sender {
	NSEvent *e = NSApp.currentEvent;
	if (e.type == NSEventTypeRightMouseUp || (e.modifierFlags & NSEventModifierFlagControl)) {
		NSMenu *m = [[NSMenu alloc] init];
		NSString *phase = bv_str(self.status[@"phase"]);
		[m addItemWithTitle:(self.panel.isVisible ? @"Hide Window" : @"Show Window") action:@selector(togglePanel:) keyEquivalent:@""].target = self;
		NSMenuItem *dict = [m addItemWithTitle:([phase isEqualToString:@"listening"] ? @"Stop Dictation" : @"Start Dictation")
		                                action:@selector(toggleClicked:) keyEquivalent:@""];
		dict.target = self;
		dict.enabled = ![phase isEqualToString:@"finishing"];
		[m addItem:[NSMenuItem separatorItem]];
		[m addItemWithTitle:@"Quit bvoice" action:@selector(quitClicked:) keyEquivalent:@""].target = self;
		self.statusItem.menu = m;
		[self.statusItem.button performClick:nil];
		self.statusItem.menu = nil;
		return;
	}
	[self togglePanel:nil];
}

- (void)togglePanel:(id)sender {
	if (self.panel.isVisible) [self.panel orderOut:nil];
	else [self showPanel];
}

- (void)showPanel {
	if (!self.positioned) {
		NSWindow *w = self.statusItem.button.window;
		NSRect sf = w ? w.frame : NSZeroRect;
		NSScreen *screen = w.screen ?: [NSScreen mainScreen];
		NSRect vis = screen.visibleFrame;
		NSRect f = self.panel.frame;
		CGFloat x = NSIsEmptyRect(sf) ? NSMaxX(vis) - f.size.width - 12 : NSMidX(sf) - f.size.width / 2;
		x = MAX(NSMinX(vis) + 8, MIN(x, NSMaxX(vis) - f.size.width - 8));
		CGFloat y = NSMaxY(vis) - f.size.height - 8;
		[self.panel setFrameOrigin:NSMakePoint(x, y)];
		self.positioned = YES;
	}
	[self.panel orderFrontRegardless];
}

- (void)toggleClicked:(id)sender { bv_send(BVToggle, nil, nil); }
- (void)syncClicked:(id)sender { bv_send(BVSync, nil, nil); }
- (void)quitClicked:(id)sender { bv_send(BVQuit, nil, nil); }

- (BOOL)windowShouldClose:(NSWindow *)sender {
	[self.panel orderOut:nil];
	return NO;
}

@end

// C entry points called from Go.

static BVController *gUI;

void bv_ui_run(int show_window) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
		gUI = [[BVController alloc] init];
		[gUI setupShowing:show_window != 0];
		dispatch_async(dispatch_get_main_queue(), ^{ goUIReady(); });
		[NSApp run];
	}
}

void bv_ui_stop(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp stop:nil];
		// -stop: takes effect after the next event, so post one
		NSEvent *e = [NSEvent otherEventWithType:NSEventTypeApplicationDefined location:NSZeroPoint modifierFlags:0
		                               timestamp:0 windowNumber:0 context:nil subtype:0 data1:0 data2:0];
		[NSApp postEvent:e atStart:YES];
	});
}

void bv_ui_set_json(int what, const char *json) {
	NSData *data = [NSData dataWithBytes:json length:strlen(json)];
	dispatch_async(dispatch_get_main_queue(), ^{
		id obj = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
		if (!gUI || !obj) return;
		switch (what) {
		case BVSetStatus:   if ([obj isKindOfClass:[NSDictionary class]]) [gUI setStatusDict:obj]; break;
		case BVSetHistory:  if ([obj isKindOfClass:[NSArray class]]) [gUI setHistoryArray:obj]; break;
		case BVSetControls: if ([obj isKindOfClass:[NSArray class]]) [gUI setControls:obj]; break;
		case BVSetNotice:   if ([obj isKindOfClass:[NSArray class]]) [gUI setNotice:obj]; break;
		}
	});
}

void bv_ui_set_on_top(int on) {
	dispatch_async(dispatch_get_main_queue(), ^{ [gUI setOnTop:on != 0]; });
}
*/
import "C"
