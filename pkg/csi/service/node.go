/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package service

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/akutz/gofsutil"
	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	devDiskID   = "/dev/disk/by-id"
	blockPrefix = "wwn-0x"
)

func (s *service) NodeStageVolume(
	ctx context.Context,
	req *csi.NodeStageVolumeRequest) (
	*csi.NodeStageVolumeResponse, error) {

	volID := req.GetVolumeId()
	volPath, err := verifyVolumeAttached(volID)
	if err != nil {
		return nil, err
	}

	// Check that block device looks good
	dev, err := getDevice(volPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"error getting block device for volume: %s, err: %s",
			volID, err.Error())
	}

	// Check that target_path is created by CO and is a directory
	target := req.GetStagingTargetPath()
	if err = verifyTargetDir(target); err != nil {
		return nil, err
	}

	//Mount if the device if needed, and if already mounted, verify compatibility
	volCap := req.GetVolumeCapability()
	fs, mntFlags, err := ensureMountVol(volCap)
	if err != nil {
		return nil, err
	}

	accMode := volCap.GetAccessMode().GetMode()
	ro := false
	if accMode == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY ||
		accMode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
		ro = true
	}

	// Get mounts to check if already staged
	mnts, err := gofsutil.GetDevMounts(context.Background(), dev.RealDev)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"could not reliably determine existing mount status: %s",
			err.Error())
	}

	if len(mnts) == 0 {
		// Device isn't mounted anywhere, stage the volume
		if fs == "" {
			fs = "ext4"
		}

		// If read-only access mode, we don't allow formatting
		if ro {
			mntFlags = append(mntFlags, "ro")
			if err := gofsutil.Mount(ctx, dev.FullPath, target, fs, mntFlags...); err != nil {
				return nil, status.Errorf(codes.Internal,
					"error with mount during staging: %s",
					err.Error())
			}
			return &csi.NodeStageVolumeResponse{}, nil
		}
		if err := gofsutil.FormatAndMount(ctx, dev.FullPath, target, fs, mntFlags...); err != nil {
			return nil, status.Errorf(codes.Internal,
				"error with format and mount during staging: %s",
				err.Error())
		}
		return &csi.NodeStageVolumeResponse{}, nil

	}
	// Device is already mounted. Need to ensure that it is already
	// mounted to the expected staging target, with correct rw/ro perms
	mounted := false
	for _, m := range mnts {
		if m.Path == target {
			mounted = true
			rwo := "rw"
			if ro {
				rwo = "ro"
			}
			if contains(m.Opts, rwo) {
				//TODO make sure that mount options match
				//log.WithFields(f).Debug(
				//	"private mount already in place")
				return &csi.NodeStageVolumeResponse{}, nil
			}
			return nil, status.Error(codes.AlreadyExists,
				"access mode conflicts with existing mount")
		}
	}
	if !mounted {
		return nil, status.Error(codes.Internal,
			"device already in use and mounted elsewhere")
	}

	return nil, nil
}

func (s *service) NodeUnstageVolume(
	ctx context.Context,
	req *csi.NodeUnstageVolumeRequest) (
	*csi.NodeUnstageVolumeResponse, error) {

	volID := req.GetVolumeId()
	volPath, err := verifyVolumeAttached(volID)
	if err != nil {
		return nil, err
	}

	target := req.GetStagingTargetPath()
	if err = verifyTargetDir(target); err != nil {
		return nil, err
	}

	// Check that block device looks good
	dev, err := getDevice(volPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"error getting block device for volume: %s, err: %s",
			volID, err.Error())
	}

	// Get mounts for device
	mnts, err := gofsutil.GetDevMounts(context.Background(), dev.RealDev)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"could not reliably determine existing mount status: %s",
			err.Error())
	}

	if len(mnts) == 0 {
		// device isn't mounted, so this has been unstaged already
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	// device is mounted. Should only be mounted to target
	if len(mnts) > 1 {
		return nil, status.Errorf(codes.Internal,
			"volume: %s appears mounted in multiple places", volID)
	}

	if mnts[0].Source == dev.RealDev && mnts[0].Path == target {
		// perfect, unstage this
		if err := gofsutil.Unmount(context.Background(), target); err != nil {
			return nil, status.Errorf(codes.Internal,
				"Error unmounting target: %s", err.Error())
		}
	} else {
		return nil, status.Errorf(codes.Internal,
			"volume %s is mounted someplace other than target: %s, mounted to: %s",
			volID, target, mnts[0].Path)
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (s *service) NodePublishVolume(
	ctx context.Context,
	req *csi.NodePublishVolumeRequest) (
	*csi.NodePublishVolumeResponse, error) {

	volID := req.GetVolumeId()
	volPath, err := verifyVolumeAttached(volID)
	if err != nil {
		return nil, err
	}

	target := req.GetTargetPath()
	// We are responsible for creating target dir, per spec
	_, err = mkdir(target)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"Unable to create target dir: %s, err: %v", target, err)
	}

	stagingTarget := req.GetStagingTargetPath()
	if err := verifyTargetDir(stagingTarget); err != nil {
		return nil, err
	}

	ro := req.GetReadonly()
	volCap := req.GetVolumeCapability()
	_, mntFlags, err := ensureMountVol(volCap)
	if err != nil {
		return nil, err
	}

	// Get underlying block device
	dev, err := getDevice(volPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"error getting block device for volume: %s, err: %s",
			volID, err.Error())
	}

	f := log.Fields{
		"volID":         volID,
		"volumePath":    dev.FullPath,
		"device":        dev.RealDev,
		"target":        target,
		"stagingTarget": stagingTarget,
	}

	// get block device mounts
	// Check if device is already mounted
	devMnts, err := getDevMounts(dev)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"could not reliably determine existing mount status: %s",
			err.Error())
	}

	// We expect that block device already staged, so there should be at least 1
	// mount already. if it's > 1, it may already be published
	if len(devMnts) > 1 {
		// check if publish is already there
		for _, m := range devMnts {
			if m.Path == target {
				// volume already published to target
				// if mount options look good, do nothing
				rwo := "rw"
				if ro {
					rwo = "ro"
				}
				if !contains(m.Opts, rwo) {
					return nil, status.Error(codes.AlreadyExists,
						"volume previously published with different options")
				}

				// Existing mount satisfies request
				log.WithFields(f).Debug("volume already published to target")
				return &csi.NodePublishVolumeResponse{}, nil
			}
		}
	} else if len(devMnts) == 0 {
		return nil, status.Errorf(codes.FailedPrecondition,
			"Volume ID: %s does not appear staged to %s", volID, stagingTarget)
	}

	// Do the bind mount to publish the volume
	if ro {
		mntFlags = append(mntFlags, "ro")
	}

	if err := gofsutil.BindMount(ctx, stagingTarget, target, mntFlags...); err != nil {
		return nil, status.Errorf(codes.Internal,
			"error publish volume to target path: %s",
			err.Error())
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (s *service) NodeUnpublishVolume(
	ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest) (
	*csi.NodeUnpublishVolumeResponse, error) {

	volID := req.GetVolumeId()
	volPath, err := verifyVolumeAttached(volID)
	if err != nil {
		return nil, err
	}

	target := req.GetTargetPath()
	if err := verifyTargetDir(target); err != nil {
		return nil, err
	}

	// Get underlying block device
	dev, err := getDevice(volPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"error getting block device for volume: %s, err: %s",
			volID, err.Error())
	}

	// get mounts
	// Check if device is already unmounted
	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"could not reliably determine existing mount status: %s",
			err.Error())
	}

	for _, m := range mnts {
		if m.Source == dev.RealDev || m.Device == dev.RealDev {
			if m.Path == target {
				err = gofsutil.Unmount(ctx, target)
				if err != nil {
					return nil, status.Errorf(codes.Internal,
						"Error unmounting target: %s", err.Error())
				} else {
					// directory should be empty
					log.WithField("path", target).Debug("removing directory")
					if err := os.Remove(target); err != nil {
						return nil, status.Errorf(codes.Internal,
							"Unable to remove target dir: %s, err: %v", target, err)
					}
				}
			}
		}
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (s *service) NodeGetVolumeStats(
	ctx context.Context,
	req *csi.NodeGetVolumeStatsRequest) (
	*csi.NodeGetVolumeStatsResponse, error) {

	return nil, nil
}

func (s *service) NodeGetCapabilities(
	ctx context.Context,
	req *csi.NodeGetCapabilitiesRequest) (
	*csi.NodeGetCapabilitiesResponse, error) {

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			&csi.NodeServiceCapability{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
		},
	}, nil
}

func (s *service) NodeGetInfo(
	ctx context.Context,
	req *csi.NodeGetInfoRequest) (
	*csi.NodeGetInfoResponse, error) {

	return nil, nil
}

// Device is a struct for holding details about a block device
type Device struct {
	FullPath string
	Name     string
	RealDev  string
}

// getDevice returns a Device struct with info about the given device, or
// an error if it doesn't exist or is not a block device
func getDevice(path string) (*Device, error) {

	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}

	// eval any symlinks and make sure it points to a device
	d, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}

	ds, err := os.Stat(d)
	if err != nil {
		return nil, err
	}
	dm := ds.Mode()
	if dm&os.ModeDevice == 0 {
		return nil, fmt.Errorf(
			"%s is not a block device", path)
	}

	return &Device{
		Name:     fi.Name(),
		FullPath: path,
		RealDev:  d,
	}, nil
}

// The files parameter is optional for testing purposes
func getDiskPath(id string, files []os.FileInfo) (string, error) {
	var (
		devs []os.FileInfo
		err  error
	)

	if files == nil {
		devs, err = ioutil.ReadDir(devDiskID)
		if err != nil {
			return "", err
		}
	} else {
		devs = files
	}

	targetDisk := blockPrefix + id

	for _, f := range devs {
		if f.Name() == targetDisk {
			return filepath.Join(devDiskID, f.Name()), nil
		}
	}

	return "", nil
}

func contains(list []string, item string) bool {
	for _, x := range list {
		if x == item {
			return true
		}
	}
	return false
}

func verifyVolumeAttached(volID string) (string, error) {
	if volID == "" {
		return "", status.Error(codes.InvalidArgument,
			"Volume ID required")
	}

	// Check that volume is attached
	volPath, err := getDiskPath(volID, nil)
	if err != nil {
		return "", status.Errorf(codes.Internal,
			"Error trying to read attached disks: %v", err)
	}
	if volPath == "" {
		return "", status.Errorf(codes.NotFound,
			"Volume ID: %s not attached to node", volID)
	}

	return volPath, nil
}

func verifyTargetDir(target string) error {
	if target == "" {
		return status.Error(codes.InvalidArgument,
			"target path required")
	}

	tgtStat, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return status.Errorf(codes.FailedPrecondition,
				"target: %s not pre-created", target)
		}
		return status.Errorf(codes.Internal,
			"failed to stat target, err: %s", err.Error())
	}

	// This check is mandated by the spec, but this would/should fail if the
	// volume has a block accessType. Maybe staging isn't intended to be used
	// with block? That would make sense you cannot share the volume for block.
	if !tgtStat.IsDir() {
		return status.Errorf(codes.FailedPrecondition,
			"existing path: %s is not a directory", target)
	}

	return nil
}

// mkdir creates the directory specified by path if needed.
// return pair is a bool flag of whether dir was created, and an error
func mkdir(path string) (bool, error) {
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.Mkdir(path, 0750); err != nil {
				log.WithField("dir", path).WithError(
					err).Error("Unable to create dir")
				return false, err
			}
			log.WithField("path", path).Debug("created directory")
			return true, nil
		}
		return false, err
	}
	if !st.IsDir() {
		return false, fmt.Errorf("existing path is not a directory")
	}
	return false, nil
}

func ensureMountVol(volCap *csi.VolumeCapability) (string, []string, error) {
	mountVol := volCap.GetMount()
	if mountVol == nil {
		return "", nil, status.Error(codes.InvalidArgument,
			"Only Mount access type supported")
	}
	fs := mountVol.GetFsType()
	mntFlags := mountVol.GetMountFlags()

	return fs, mntFlags, nil
}

// a wrapper around gofsutil.GetMounts that handles bind mounts
func getDevMounts(
	sysDevice *Device) ([]gofsutil.Info, error) {

	ctx := context.Background()
	devMnts := make([]gofsutil.Info, 0)

	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return devMnts, err
	}
	for _, m := range mnts {
		if m.Device == sysDevice.RealDev || (m.Device == "devtmpfs" && m.Source == sysDevice.RealDev) {
			devMnts = append(devMnts, m)
		}
	}
	return devMnts, nil
}
