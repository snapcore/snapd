// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package builtin

const nvidiaTegraDriversSupportSummary = `allows hardware access to NVIDIA tegra platforms`

const nvidiaTegraDriversSupportBaseDeclarationSlots = `
  nvidia-tegra-drivers-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const nvidiaTegraDriversSupportConnectedPlugAppArmor = `
# This is inverse of
# https://forum.snapcraft.io/t/call-for-testing-chromium-62-0-3202-62/2569/46
# As nvidia-assemble snap needs to create the static & dynamic MAJOR
# chrdevs for all other snaps to have access to. Specifically
/{,usr/}bin/mknod ixr,
allow capability mknod,

# To read dynamically allocated MAJOR for nvidia-uvm
@{PROC}/modules r,
@{PROC}/devices r,
@{PROC}/driver/nvidia/params r,
@{PROC}/driver/nvidia/capabilities/mig/monitor r,
@{PROC}/driver/nvidia/capabilities/mig/config r,
@{PROC}/sys/vm/mmap_min_addr r,

/sys/devices/soc0/platform r,
/sys/devices/soc0/soc_id r,
/sys/devices/soc0/revision r,
/sys/devices/soc0/major r,
/sys/devices/platform/bus@0/3810000.fuse/fuse/nvmem r,

/dev/nvmap rw,
/dev/dri/renderD128 rw,
/dev/nvgpu/igpu0/power rw,
/dev/nvgpu/igpu0/ctrl rw,
/dev/nvgpu/igpu0/prof rw,
/dev/char/498:1 rw,
/dev/char/498:2 rw,
/dev/host1x-fence rw,
/dev/shm/memmap_ipc_shm rw,

/sys/module/firmware_class/parameters/path rw,
`

var nvidiaTegraDriversSupportConnectedPlugUdev = []string{
	`KERNEL=="device-mapper", NAME="mapper/control"`,
	`SUBSYSTEM!="block", GOTO="dm_end"`,
	`KERNEL!="dm-[0-9]*", GOTO="dm_end"`,
	`ACTION!="add|change", GOTO="dm_end"`,
	`ENV{DM_UDEV_PRIMARY_SOURCE_FLAG}="1"`,
	`LABEL="dm_end"`,

	`ACTION=="add" SUBSYSTEM=="sdio" ATTR{vendor}=="0x02d0" RUN+="/etc/systemd/nvwifibt-pre.sh register $attr{device}"`,
	`ACTION=="add" SUBSYSTEM=="pci" ATTR{vendor}=="0x14e4" RUN+="/etc/systemd/nvwifibt-pre.sh register $attr{device}"`,

	`ACTION=="change" SUBSYSTEM=="rfkill" ATTR{name}=="bluedroid_pm*" ATTR{state}=="1" RUN+="/bin/systemctl start nvwifibt.service"`,
	`ACTION=="change" SUBSYSTEM=="rfkill" ATTR{name}=="bluedroid_pm*" ATTR{state}=="0" RUN+="/bin/systemctl stop nvwifibt.service"`,

	`ACTION=="remove" GOTO="nvidia_end"`,
	`KERNEL=="camera.pcl", RUN+="/usr/sbin/camera_device_detect"`,

	`KERNEL=="knvrm" OWNER="root" GROUP="root" MODE="0660"`,
	`KERNEL=="knvmap" OWNER="root" GROUP="root" MODE="0660"`,

	`DEVPATH=="/module/nvidia", ACTION=="add", RUN+="/bin/mknod -m 666 /dev/nvidiactl c 195 255"`,
	`DEVPATH=="/module/nvidia", ACTION=="add", RUN+="/bin/mknod -m 666 /dev/nvidia0 c 195 0"`,
	`DEVPATH=="/module/nvidia_modeset", ACTION=="add", RUN+="/bin/mknod -m 666 /dev/nvidia-modeset c 195 254"`,

	`KERNEL=="15480000.nvdec", DRIVER=="tegra-nvdec", ACTION=="bind", RUN+="/bin/mknod -m 666 /dev/v4l2-nvdec c 1 3"`,
	`KERNEL=="154c0000.nvenc", DRIVER=="tegra-nvenc", ACTION=="bind", RUN+="/bin/mknod -m 666 /dev/v4l2-nvenc c 1 3"`,

	`KERNEL=="nvmap" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="dma_heap" KERNEL=="system_heap" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="dma_heap" KERNEL=="system" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="host1x" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="nvram" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="nvhdcp*" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="nvhost*" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="nvhost-ctxsw-gpu" OWNER="root" GROUP="debug" MODE="0660"`,
	`KERNEL=="nvhost-dbg-gpu" OWNER="root" GROUP="debug" MODE="0660"`,
	`KERNEL=="nvhost-prof-ctx-gpu" OWNER="root" GROUP="debug" MODE="0660"`,
	`KERNEL=="nvhost-prof-dev-gpu" OWNER="root" GROUP="debug" MODE="0660"`,
	`KERNEL=="nvhost-prof-gpu" OWNER="root" GROUP="debug" MODE="0660"`,
	`KERNEL=="nvhost-sched-gpu" OWNER="root" GROUP="root" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2-power" KERNEL=="power" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="as" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="channel" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="ctrl" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="ctxsw" OWNER="root" GROUP="debug" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="dbg" OWNER="root" GROUP="debug" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="prof" OWNER="root" GROUP="debug" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="prof-ctx" OWNER="root" GROUP="debug" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="prof-dev" OWNER="root" GROUP="debug" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="sched" OWNER="root" GROUP="root" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="tsg" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="nvsched" OWNER="root" GROUP="video" MODE="0640"`,
	`SUBSYSTEM=="nvidia-pci-gpu-v2-power" KERNEL=="power" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="nvidia-pci-gpu-v2" KERNEL=="as" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="nvidia-pci-gpu-v2" KERNEL=="channel" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="nvidia-pci-gpu-v2" KERNEL=="ctrl" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="nvidia-pci-gpu-v2" KERNEL=="ctxsw" OWNER="root" GROUP="debug" MODE="0660"`,
	`SUBSYSTEM=="nvidia-pci-gpu-v2" KERNEL=="dbg" OWNER="root" GROUP="debug" MODE="0660"`,
	`SUBSYSTEM=="nvidia-pci-gpu-v2" KERNEL=="prof" OWNER="root" GROUP="debug" MODE="0660"`,
	`SUBSYSTEM=="nvidia-pci-gpu-v2" KERNEL=="prof-ctx" OWNER="root" GROUP="debug" MODE="0660"`,
	`SUBSYSTEM=="nvidia-pci-gpu-v2" KERNEL=="prof-dev" OWNER="root" GROUP="debug" MODE="0660"`,
	`SUBSYSTEM=="nvidia-pci-gpu-v2" KERNEL=="sched" OWNER="root" GROUP="root" MODE="0660"`,
	`SUBSYSTEM=="nvidia-pci-gpu-v2" KERNEL=="tsg" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="nvidia-pci-gpu-v2" KERNEL=="nvsched" OWNER="root" GROUP="video" MODE="0640"`,
	`KERNEL=="tegra_camera_ctrl" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="tegra_cec" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="tegra_dc*" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="tegra_mipi_cal" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="tegra-vi*" OWNER="root" GROUP="video" MODE="0660"`,

	`KERNEL=="tegra-soc-hwpm" OWNER="root" GROUP="debug" MODE="0660"`,

	`KERNEL=="torch" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="ov*" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="focuser*" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="camera*" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="imx*" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="sh5*" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="tps*" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="mipi-cal" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="ar*" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="camchar*" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="capture-*" OWNER="root" GROUP="video" MODE="0660"`,
	`KERNEL=="cdi_tsc" OWNER="root" GROUP="video" MODE="0660"`,

	`LABEL="nvidia_end"`,
	`KERNEL=="mmcblk[0-9]", SUBSYSTEMS=="mmc",ACTION=="add|change", ATTR{bdi/read_ahead_kb}="2048"`,
}

const nvidiaTegraDriversSupportConnectedPlugSecComp = `
bind
`

type nvidiaTegraDriversSupportInterface struct {
	commonInterface
}

func init() {
	registerIface(&nvidiaTegraDriversSupportInterface{commonInterface: commonInterface{
		name:                  "nvidia-tegra-drivers-support",
		summary:               nvidiaTegraDriversSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  nvidiaTegraDriversSupportBaseDeclarationSlots,
		connectedPlugAppArmor: nvidiaTegraDriversSupportConnectedPlugAppArmor,
		connectedPlugUDev:     nvidiaTegraDriversSupportConnectedPlugUdev,
		connectedPlugSecComp:  nvidiaTegraDriversSupportConnectedPlugSecComp,
	}})
}
