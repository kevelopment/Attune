//go:build (darwin || linux) && libmtp && cgo

#include "mtp_libmtp_progress.h"

extern int goAttuneMTPProgress(uint64_t done, uint64_t total, uintptr_t handle);

static int attune_mtp_progress(uint64_t const done, uint64_t const total,
                               void const * const userdata) {
  const attune_mtp_progress_context *context = userdata;
  if (context != NULL && context->handle != 0) {
    return goAttuneMTPProgress(done, total, context->handle);
  }
  return 0;
}

int attune_mtp_get_file(LIBMTP_mtpdevice_t *device, uint32_t id,
                        const char *path,
                        attune_mtp_progress_context *progress) {
  return LIBMTP_Get_File_To_File(device, id, path,
                                  progress == NULL ? NULL : attune_mtp_progress,
                                  progress);
}

int attune_mtp_send_file(LIBMTP_mtpdevice_t *device, const char *path,
                         LIBMTP_file_t *metadata,
                         attune_mtp_progress_context *progress) {
  return LIBMTP_Send_File_From_File(device, path, metadata,
                                     progress == NULL ? NULL : attune_mtp_progress,
                                     progress);
}
