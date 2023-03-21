#include <stdio.h>
#include <unistd.h>
#include <stdlib.h>
#include <stdbool.h>
#include <string.h>
#include <fcntl.h>
#include <poll.h>
#include <linux/perf_event.h>
#include <linux/bpf.h>
#include <errno.h>
#include <assert.h>
#include <sys/syscall.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <time.h>
#include <signal.h>
#include <libbpf.h>
#include "bpf_load.h"
#include "perf-sys.h"
#include "trace_helpers.h"

static int pmu_fd;


#define MAX_CNT 100000ll

#define NOTIFY_COMMON_HDR \
	__u8		type;		\
	__u8		subtype;	\
	__u16		source;		\
	__u32		hash;

#define NOTIFY_CAPTURE_HDR \
	NOTIFY_COMMON_HDR						\
	__u32		len_orig;	/* Length of original packet */	\
	__u16		len_cap;	/* Length of captured bytes */	\
	__u16		version;	/* Capture header version */

union v6addr {
	struct {
		__u32 p1;
		__u32 p2;
		__u32 p3;
		__u32 p4;
	};
	struct {
		__u64 d1;
		__u64 d2;
	};
	__u8 addr[16];
};

struct trace_notify {
	NOTIFY_CAPTURE_HDR
	__u32		src_label;
	__u32		dst_label;
	__u16		dst_id;
	__u8		reason;
	__u8		ipv6:1;
	__u8		pad:7;
	__u32		ifindex;
	union {
		struct {
			__be32		orig_ip4;
			__u32		orig_pad1;
			__u32		orig_pad2;
			__u32		orig_pad3;
		};
		union v6addr	orig_ip6;
	};
};

static int print_bpf_output(void *data, int size)
{
	struct trace_notify *e = data;

  printf("get event %d, %d. ", e->src_label, e->dst_label);


	return LIBBPF_PERF_EVENT_CONT;
}

static const char *file_path = "/sys/fs/bpf/tc/globals/cilium_events";

static void load_bpf_perf_event(void)
{
	struct perf_event_attr attr = {
		.sample_type = PERF_SAMPLE_RAW,
		.type = PERF_TYPE_SOFTWARE,
		.config = PERF_COUNT_SW_BPF_OUTPUT,
	};
	int key = 0, fd;

  fd = bpf_obj_get(file_path);
  if (fd < 0) {
    printf("Failed to fetch the map: %d (%s)\n", fd, strerror(errno));
    return;
  }

	pmu_fd = sys_perf_event_open(&attr, -1/*pid*/, 0/*cpu*/, -1/*group_fd*/, 0);

	assert(pmu_fd >= 0);
	assert(bpf_map_update_elem(fd, &key, &pmu_fd, BPF_ANY) == 0);
	ioctl(pmu_fd, PERF_EVENT_IOC_ENABLE, 0);
}

int main(int argc, char **argv)
{
	int ret;

	load_bpf_perf_event();

	if (perf_event_mmap(pmu_fd) < 0)
		return 1;

	ret = perf_event_poller(pmu_fd, print_bpf_output);
	return ret;
}
