#include <stddef.h>
#include <stdint.h>
typedef struct { size_t offset; size_t length; } rust_span;
int rust_scan(const unsigned char*,size_t,uint32_t,int,rust_span*);
int rust_project(const unsigned char*,size_t,uint32_t,int,rust_span*);
int rust_batch(const unsigned char*,const size_t*,size_t,uint32_t,int,rust_span*);
int rust_noop(void);
