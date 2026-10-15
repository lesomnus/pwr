#include <stddef.h>
#include <stdint.h>
typedef struct { size_t offset; size_t length; } lab_span;
int lab_scan(const unsigned char*,size_t,uint32_t,int,lab_span*);
int lab_project(const unsigned char*,size_t,uint32_t,int,lab_span*);
int lab_batch(const unsigned char*,const size_t*,size_t,uint32_t,int,lab_span*);
int lab_noop(void);
