// Corpus fixture: not compiled, not run.

// An Apple framework, which arrives with the SDK and is not a dependency anybody patches
// separately. Recognised by its first path segment being a framework name, which is the only
// thing separating it from `<gtest_extras/matchers.h>` next door in the C++ fixture.
#import <Foundation/Foundation.h>

/** A line-oriented reader over a file. */
@protocol CorpusReadable <NSObject>
- (NSString *)readLine;
@end

@interface CorpusReader : NSObject <CorpusReadable>

@property (nonatomic, copy, readonly) NSString *path;

- (instancetype)initWithPath:(NSString *)path;

// A selector is interleaved with its parameters, and every part is the method's name.
// Recording only the first would collapse `setName:` and `setName:age:` into one method, and
// they are two methods.
- (void)setName:(NSString *)name age:(NSInteger)age;

+ (instancetype)readerWithPath:(NSString *)path encoding:(NSStringEncoding)encoding;

@end

// A category reopens a type declared elsewhere and adds methods to it. The methods belong to
// NSString; the category itself declares no type, and recording one would put a type on the
// page that this file does not define.
@interface NSString (CorpusReaderExtras)
- (BOOL)corpusLooksLikePath;
@end
