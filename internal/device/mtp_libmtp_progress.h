#ifndef ATTUNE_LIBMTP_PROGRESS_H
#define ATTUNE_LIBMTP_PROGRESS_H

#include <stdint.h>
#include <libmtp.h>

typedef struct {
  uintptr_t handle;
} attune_mtp_progress_context;

int attune_mtp_get_file(LIBMTP_mtpdevice_t *device, uint32_t id,
                        const char *path,
                        attune_mtp_progress_context *progress);
int attune_mtp_send_file(LIBMTP_mtpdevice_t *device, const char *path,
                         LIBMTP_file_t *metadata,
                         attune_mtp_progress_context *progress);

#endif
