#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>

static NSImage *attuneDarkIcon;
static NSImage *attuneLightIcon;

static void attune_apply_app_icon(void) {
    NSAppearanceName match = [[NSApp effectiveAppearance] bestMatchFromAppearancesWithNames:@[NSAppearanceNameAqua, NSAppearanceNameDarkAqua]];
    [NSApp setApplicationIconImage:[match isEqualToString:NSAppearanceNameDarkAqua] ? attuneDarkIcon : attuneLightIcon];
}

@interface AttuneAppIconObserver : NSObject
@end

@implementation AttuneAppIconObserver
- (void)observeValueForKeyPath:(NSString *)keyPath ofObject:(id)object change:(NSDictionary *)change context:(void *)context {
    dispatch_async(dispatch_get_main_queue(), ^{
        attune_apply_app_icon();
    });
}
@end

void attune_follow_system_app_icon(const void *dark, int dark_len, const void *light, int light_len) {
    @autoreleasepool {
        // Copy the bytes now: the Go buffers are only guaranteed for the duration of this call.
        NSData *darkData = [NSData dataWithBytes:dark length:dark_len];
        NSData *lightData = [NSData dataWithBytes:light length:light_len];
        dispatch_async(dispatch_get_main_queue(), ^{
            if (attuneDarkIcon != nil) {
                return;
            }
            attuneDarkIcon = [[NSImage alloc] initWithData:darkData];
            attuneLightIcon = [[NSImage alloc] initWithData:lightData];
            if (attuneDarkIcon == nil || attuneLightIcon == nil) {
                return;
            }
            attune_apply_app_icon();
            [NSApp addObserver:[[AttuneAppIconObserver alloc] init] forKeyPath:@"effectiveAppearance" options:0 context:NULL];
        });
    }
}
