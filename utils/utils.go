package utils

// #include "limits.h"
import "C"

import (
	"net"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	log "github.com/Sirupsen/logrus"
	"github.com/gluster/glusterd2/errors"
	"github.com/pborman/uuid"
)

const (
	testXattr     = "trusted.glusterfs.test"
	volumeIDXattr = "trusted.glusterfs.volume-id"
	gfidXattr     = "trusted.gfid"
)

var (
	// PathMax calls unix.PathMax
	PathMax = unix.PathMax
	// Removexattr calls unix.Removexattr
	Removexattr = unix.Removexattr
	// Setxattr calls unix.Setxattr
	Setxattr = unix.Setxattr
	// Getxattr calls unix.Getxattr
	Getxattr = unix.Getxattr
)

//PosixPathMax represents C's POSIX_PATH_MAX
const PosixPathMax = C._POSIX_PATH_MAX

// IsLocalAddress checks whether a given host/IP is local
// Does lookup only after string matching IP addresses
func IsLocalAddress(address string) (bool, error) {
	var host string

	host, _, _ = net.SplitHostPort(address)
	if host == "" {
		host = address
	}

	localNames := []string{"127.0.0.1", "localhost", "::1"}
	for _, name := range localNames {
		if host == name {
			return true, nil
		}
	}

	laddrs, e := net.InterfaceAddrs()
	if e != nil {
		return false, e
	}
	var lips []net.IP
	for _, laddr := range laddrs {
		lipa := laddr.(*net.IPNet)
		lips = append(lips, lipa.IP)
	}

	for _, ip := range lips {
		if host == ip.String() {
			return true, nil
		}
	}

	rips, e := net.LookupIP(host)
	if e != nil {
		return false, e
	}
	for _, rip := range rips {
		for _, lip := range lips {
			if lip.Equal(rip) {
				return true, nil
			}
		}
	}
	return false, nil
}

// ParseHostAndBrickPath parses the host & brick path out of req.Bricks list
func ParseHostAndBrickPath(brickPath string) (string, string, error) {
	i := strings.LastIndex(brickPath, ":")
	if i == -1 {
		log.WithField("brick", brickPath).Error(errors.ErrInvalidBrickPath.Error())
		return "", "", errors.ErrInvalidBrickPath
	}
	hostname := brickPath[0:i]
	path := brickPath[i+1:]

	return hostname, path, nil
}

//ValidateBrickPathLength validates the length of the brick path
func ValidateBrickPathLength(brickPath string) error {
	//TODO : Check whether PATH_MAX is compatible across all distros
	if len(filepath.Clean(brickPath)) >= PathMax {
		log.WithField("brick", brickPath).Error(errors.ErrBrickPathTooLong.Error())
		return errors.ErrBrickPathTooLong
	}
	return nil
}

//ValidateBrickSubDirLength validates the length of each sub directories under
//the brick path
func ValidateBrickSubDirLength(brickPath string) error {
	subdirs := strings.Split(brickPath, string(os.PathSeparator))
	// Iterate over the sub directories and validate that they don't breach
	//  _POSIX_PATH_MAX validation
	for _, subdir := range subdirs {
		if len(subdir) >= PosixPathMax {
			log.WithField("subdir", subdir).Error("sub directory path is too long")
			return errors.ErrSubDirPathTooLong
		}
	}
	return nil
}

//GetDeviceID fetches the device id of the device containing the file/directory
func GetDeviceID(f os.FileInfo) (int, error) {
	s := f.Sys()
	switch s := s.(type) {
	//TODO : Need to change syscall to unix, using unix.Stat_t fails in one
	//of the test
	case *syscall.Stat_t:
		return int(s.Dev), nil
	}
	return -1, errors.ErrDeviceIDNotFound
}

//ValidateBrickPathStats checks whether the brick directory can be created with
//certain validations like directory checks, whether directory is part of mount
//point etc
func ValidateBrickPathStats(brickPath string, host string, force bool) error {
	var created bool
	var rootStat, brickStat, parentStat os.FileInfo
	err := os.MkdirAll(brickPath, os.ModeDir|os.ModePerm)
	if err != nil {
		if !os.IsExist(err) {
			log.WithFields(log.Fields{
				"host":  host,
				"brick": brickPath,
			}).Error("Failed to create brick - ", err.Error())
			return err
		}
	} else {
		created = true
	}
	brickStat, err = os.Lstat(brickPath)
	if err != nil {
		log.WithFields(log.Fields{
			"host":  host,
			"brick": brickPath,
		}).Error("Failed to stat on brick path - ", err.Error())
		return err
	}
	if !created && !brickStat.IsDir() {
		log.WithFields(log.Fields{
			"host":  host,
			"brick": brickPath,
		}).Error("brick path which is already present is not a directory")
		return errors.ErrBrickNotDirectory
	}

	rootStat, err = os.Lstat("/")
	if err != nil {
		log.Error("Failed to stat on / -", err.Error())
		return err
	}

	parentBrick := path.Dir(brickPath)
	parentStat, err = os.Lstat(parentBrick)
	if err != nil {
		log.WithFields(log.Fields{
			"host":        host,
			"brick":       brickPath,
			"parentBrick": parentBrick,
		}).Error("Failed to stat on parent of the brick path")
		return err
	}

	if !force {
		var parentDeviceID, rootDeviceID, brickDeviceID int
		var e error
		parentDeviceID, e = GetDeviceID(parentStat)
		if e != nil {
			log.WithFields(log.Fields{
				"host":  host,
				"brick": brickPath,
			}).Error("Failed to find the device id for parent of brick path")

			return err
		}
		rootDeviceID, e = GetDeviceID(rootStat)
		if e != nil {
			log.Error("Failed to find the device id of '/'")
			return err
		}
		brickDeviceID, e = GetDeviceID(brickStat)
		if e != nil {
			log.WithFields(log.Fields{
				"host":  host,
				"brick": brickPath,
			}).Error("Failed to find the device id of the brick")
			return err
		}
		if brickDeviceID != parentDeviceID {
			log.WithFields(log.Fields{
				"host":  host,
				"brick": brickPath,
			}).Error(errors.ErrBrickIsMountPoint.Error())
			return errors.ErrBrickIsMountPoint
		} else if parentDeviceID == rootDeviceID {
			log.WithFields(log.Fields{
				"host":  host,
				"brick": brickPath,
			}).Error(errors.ErrBrickUnderRootPartition.Error())
			return errors.ErrBrickUnderRootPartition
		}

	}

	// Workaround till https://review.gluster.org/#/c/18003/ gets in
	if err := os.MkdirAll(filepath.Join(brickPath, ".glusterfs", "indices"), os.ModeDir|os.ModePerm); err != nil {
		log.WithError(err).Error("failed to create .glusterfs/indices directory")
		return err
	}

	return nil
}

//ValidateXattrSupport checks whether the underlying file system has extended
//attribute support and it also sets some internal xattrs to mark the brick in
//use
func ValidateXattrSupport(brickPath string, host string, volid uuid.UUID, force bool) error {
	var err error
	err = Setxattr(brickPath, "trusted.glusterfs.test", []byte("working"), 0)
	if err != nil {
		log.WithFields(log.Fields{"error": err.Error(),
			"brickPath": brickPath,
			"host":      host,
			"xattr":     testXattr}).Error("setxattr failed")
		return err
	}
	err = Removexattr(brickPath, "trusted.glusterfs.test")
	if err != nil {
		log.WithFields(log.Fields{"error": err.Error(),
			"brickPath": brickPath,
			"host":      host,
			"xattr":     testXattr}).Error("removexattr failed")
		return err
	}
	if !force {
		if isBrickPathAlreadyInUse(brickPath) {
			log.WithFields(log.Fields{
				"brickPath": brickPath,
				"host":      host}).Error(errors.ErrBrickPathAlreadyInUse.Error())
			return errors.ErrBrickPathAlreadyInUse
		}
	}
	err = Setxattr(brickPath, volumeIDXattr, []byte(volid), 0)
	if err != nil {
		log.WithFields(log.Fields{"error": err.Error(),
			"brickPath": brickPath,
			"host":      host,
			"xattr":     volumeIDXattr}).Error("setxattr failed")
		return err
	}

	return nil
}

func isBrickPathAlreadyInUse(brickPath string) bool {
	keys := []string{gfidXattr, volumeIDXattr}
	var p string
	var buf []byte
	p = brickPath
	for ; p != "/"; p = path.Dir(p) {
		for _, key := range keys {
			size, err := Getxattr(p, key, buf)
			if err != nil {
				return false
			} else if size > 0 {
				return true
			} else {
				return false
			}

		}
	}
	return false
}

// InitDir creates directory path and checks if files can be created in it.
// Returns error if path is not a directory or if directory doesn't have
// write permission.
func InitDir(path string) error {

	if err := os.MkdirAll(path, os.ModeDir|os.ModePerm); err != nil {
		log.WithError(err).WithField("path", path).Debug(
			"failed to create directory")
		return err
	}

	if err := unix.Access(path, unix.W_OK); err != nil {
		log.WithError(err).WithField("path", path).Debug(
			"directory does not have write permission")
		return err
	}

	return nil
}

// GetLocalIP will give local IP address of this node
func GetLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, address := range addrs {
		// check the address type and if it is not a loopback then return it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}
	return "", errors.ErrIPAddressNotFound
}

// GetFuncName returns the name of the passed function pointer
func GetFuncName(fn interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
}

// StringInSlice will return true if the given string is present in the
// list of strings provided. Will return false otherwise.
func StringInSlice(query string, list []string) bool {
	for _, s := range list {
		if s == query {
			return true
		}
	}
	return false
}

// IsAddressSame checks is two host addresses are same
func IsAddressSame(host1, host2 string) bool {

	if host1 == host2 {
		return true
	}

	addrs1, err := net.LookupHost(host1)
	if err != nil {
		return false
	}

	addrs2, err := net.LookupHost(host2)
	if err != nil {
		return false
	}

	for _, a := range addrs1 {
		if StringInSlice(a, addrs2) {
			return true
		}
	}

	return false
}
