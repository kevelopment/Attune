#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>

char *attune_read_clipboard_file_urls(void) {
    @autoreleasepool {
        NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
        NSArray<NSURL *> *urls = [pasteboard readObjectsForClasses:@[[NSURL class]]
                                                           options:@{ NSPasteboardURLReadingFileURLsOnlyKey: @YES }];
        NSMutableArray<NSString *> *paths = [NSMutableArray array];
        for (NSURL *url in urls) {
            if ([url isFileURL]) {
                [paths addObject:[url path]];
                if ([paths count] > 500) {
                    return NULL;
                }
            }
        }
        NSData *json = [NSJSONSerialization dataWithJSONObject:paths options:0 error:nil];
        if (json == nil) {
            return NULL;
        }
        char *result = malloc([json length] + 1);
        if (result == NULL) {
            return NULL;
        }
        memcpy(result, [json bytes], [json length]);
        result[[json length]] = '\0';
        return result;
    }
}
