// Corpus fixture: not compiled, not run.

#import <Foundation/Foundation.h>

// The quoted form, resolved against this file's own directory.
#import "Reader.h"

// The near-miss for Objective-C, on the framework boundary. `CorpusKit` is spelled exactly
// like a framework include and is not one — nothing here declares it and no SDK ships it. A
// rule that took any capitalised first segment for a framework would classify it as the
// platform, and a dependency classified as the platform disappears from the coverage report.
#import <CorpusKit/CorpusKit.h>

@interface CorpusReader ()
// A class extension reopens the class to declare what the header does not expose.
- (BOOL)validate;
@end

@implementation CorpusReader

- (instancetype)initWithPath:(NSString *)path
{
    self = [super init];
    if (self) {
        _path = [path copy];
    }
    return self;
}

- (NSString *)readLine
{
    // A message send has the shape a method declaration is told apart from, and the leading
    // sign is the whole difference.
    if (![self validate]) {
        return nil;
    }
    NSString *line = [NSString stringWithFormat:@"%@", self.path];
    return line;
}

- (void)setName:(NSString *)name age:(NSInteger)age
{
    _path = [name copy];
}

+ (instancetype)readerWithPath:(NSString *)path encoding:(NSStringEncoding)encoding
{
    return [[CorpusReader alloc] initWithPath:path];
}

- (BOOL)validate
{
    return self.path.length > 0;
}

// `@end` closes the scope, and it is not a brace. An interface left open would claim every
// later method as its own.
@end

@implementation NSString (CorpusReaderExtras)

- (BOOL)corpusLooksLikePath
{
    return [self containsString:@"/"];
}

@end
