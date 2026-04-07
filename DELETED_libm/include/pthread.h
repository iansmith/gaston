/* pthread.h — minimal stub for gaston C compiler */
#ifndef _PTHREAD_H
#define _PTHREAD_H

#define PTHREAD_CANCEL_DISABLE 0
#define PTHREAD_CANCEL_ENABLE  1

int pthread_setcancelstate(int state, int *oldstate);

#endif /* _PTHREAD_H */
