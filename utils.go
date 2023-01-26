package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/apex/log"
	"github.com/hashicorp/go-version"
	rpmversion "github.com/knqyf263/go-rpm-version"
	"github.com/msoap/byline"
	"github.com/pkg/errors"
	"github.com/project-stacker/stacker/mount"
)

const CoreVGName = "vg_ifc0"

func RunCommand(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Errorf("%s: %s: %s", strings.Join(args, " "), err, string(output))
	}
	return nil
}

func RunCommandEnv(env []string, args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Errorf("%s: %s: %s", strings.Join(args, " "), err, string(output))
	}
	return nil
}

func RunCommandWithRc(args ...string) ([]byte, int) {
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	return out, GetCommandErrorRC(err)
}

func RunCommandWithOutputErrorRc(args ...string) ([]byte, []byte, int) {
	cmd := exec.Command(args[0], args[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), GetCommandErrorRC(err)
}

func Which(commandName string) string {
	return WhichInRoot(commandName, "")
}

func WhichInRoot(commandName string, root string) string {
	cmd := []string{"sh", "-c", "command -v \"$0\"", commandName}
	if root != "" && root != "/" {
		cmd = append([]string{"chroot", root}, cmd...)
	}
	out, rc := RunCommandWithRc(cmd...)
	if rc == 0 {
		return strings.TrimSuffix(string(out), "\n")
	}
	if rc != 127 {
		log.Warnf("checking for %s exited unexpected value %d\n", commandName, rc)
	}
	return ""
}

func GetCommandErrorRCDefault(err error, rcError int) int {
	if err == nil {
		return 0
	}
	exitError, ok := err.(*exec.ExitError)
	if ok {
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	log.Debugf("Unavailable return code for %s. returning %d", err, rcError)
	return rcError
}

func GetCommandErrorRC(err error) int {
	return GetCommandErrorRCDefault(err, 127)
}

func LogCommand(args ...string) error {
	return LogCommandWithFunc(log.Infof, args...)
}

func LogCommandDebug(args ...string) error {
	return LogCommandWithFunc(log.Debugf, args...)
}

func LogCommandWithFunc(logf func(string, ...interface{}), args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		logf("%s-fail | %s", err)
		return err
	}
	cmd.Stderr = cmd.Stdout
	err = cmd.Start()
	if err != nil {
		logf("%s-fail | %s", args[0], err)
		return err
	}
	pid := cmd.Process.Pid
	logf("|%d-start| %q", pid, args)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err := byline.NewReader(stdoutPipe).Each(
			func(line []byte) {
				logf("|%d-out  | %s", pid, line[:len(line)-1])
			}).Discard()
		if err != nil {
			log.Fatalf("Unexpected %s", err)
		}
		wg.Done()
	}()

	wg.Wait()
	err = cmd.Wait()

	logf("|%d-exit | rc=%d", pid, GetCommandErrorRC(err))
	return err
}

// FindProcesses - return a list of PID that match provided regexp.
// regex is compared to a space-separated string of arguments found in /proc/pid/cmdline.
func FindProcesses(m *regexp.Regexp) ([]int, error) {
	// [0-9] keeps 'self' and some others from matching.
	var cmdline string
	var pid int
	var found = []int{}
	files, err := filepath.Glob("/proc/[0-9]*/cmdline")
	if err != nil {
		return found, errors.Wrapf(err, "Failed to list files in /proc/[0-9]*/cmdline")
	}

	for _, f := range files {
		content, err := ioutil.ReadFile(f)
		if os.IsNotExist(err) {
			// process executed after glob reading.
			continue
		}
		cmdline = strings.Join(strings.Split(string(content), "\x00"), " ")
		if !m.MatchString(cmdline) {
			continue
		}

		ptoks := strings.Split(f, "/")
		if len(ptoks) != 4 {
			return found, fmt.Errorf("path %s split on / had '%d' tokens. Expected 4", f, len(ptoks))
		}
		pid, err = strconv.Atoi(ptoks[2])
		if err != nil {
			return found, fmt.Errorf("Failed to parse int on '%s' from path '%s'", ptoks[2], f)
		}
		found = append(found, pid)
	}

	return found, err

}

func EnsureDir(dir string) error {
	return errors.Wrap(os.MkdirAll(dir, 0755), "couldn't make dirs")
}

// CopyFileBits - copy file content from a to b
// differs from CopyFile in:
//   - does not do permissions - new files created with 0644
//   - if src is a symlink, copies content, not link.
//   - does not invoke sh.
func CopyFileBits(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}

// Copy one file to a new path, i.e. cp a b
func CopyFile(src, dest string) error {
	if err := EnsureDir(filepath.Dir(src)); err != nil {
		return err
	}
	if err := EnsureDir(filepath.Dir(dest)); err != nil {
		return err
	}
	cmdtxt := fmt.Sprintf("cp -fa %s %s", src, dest)
	return RunCommand("sh", "-c", cmdtxt)
}

// Copy a set of files to a target directory, i.e. cp * dir/
func CopyFiles(src, dest string) error {
	if err := EnsureDir(filepath.Dir(src)); err != nil {
		return err
	}
	if err := EnsureDir(dest); err != nil {
		return err
	}
	cmdtxt := fmt.Sprintf("cp -fa %s %s", src, dest)
	return RunCommand("sh", "-c", cmdtxt)
}

func RsyncDirWithErrorQuiet(src, dest string) error {
	err := LogCommand("rsync", "--quiet", "--archive", src+"/", dest+"/")
	if err != nil {
		return errors.Wrapf(err, "Failed copying %s to %s\n", src, dest)
	}
	return nil
}

func RsyncDirWithError(src, dest string) error {
	err := LogCommand("rsync", "--verbose", "--archive", src+"/", dest+"/")
	if err != nil {
		return errors.Wrapf(err, "Failed copying %s to %s\n", src, dest)
	}
	return nil
}

func RsyncDir(src, dest string) {
	if err := RsyncDirWithError(src, dest); err != nil {
		log.Warnf("RsyncDir failed with error, but that is to be ignored: %v", err)
	}
}

func GetDevMounts(devPath string) ([]string, error) {
	if devPath == "" {
		return []string{}, nil
	}
	mounts, err := mount.ParseMounts("/proc/self/mountinfo")
	if err != nil {
		return []string{}, err
	}

	mps := []string{}
	for _, mount := range mounts {
		if mount.Source == devPath {
			mps = append(mps, mount.Target)
		}
	}

	return mps, nil
}

func UnmountDev(devPath string) error {
	mps, err := GetDevMounts(devPath)
	if err != nil {
		return err
	}
	for _, mp := range mps {
		if err := syscall.Unmount(mp, 0); err != nil {
			return fmt.Errorf("Failed to unmount %s mounted at %s: %v", devPath, mp, err)
		}
	}
	return nil
}

func IsMountpoint(path string) (bool, error) {
	return IsMountpointOfDevice(path, "")
}

func IsMountpointOfDevice(path, devicepath string) (bool, error) {
	path = strings.TrimSuffix(path, "/")
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) <= 1 {
			continue
		}
		if (fields[1] == path || path == "") && (fields[0] == devicepath || devicepath == "") {
			return true, nil
		}
	}

	return false, nil
}

func MountFile(source, dest string) (func(), error) {
	if !PathExists(dest) {
		parent := path.Dir(dest)
		if err := EnsureDir(parent); err != nil {
			return func() {}, err
		}
		f, err := os.Create(dest)
		if err != nil {
			return func() {}, err
		}
		f.Close()
	}
	err := syscall.Mount(source, dest, "none", syscall.MS_BIND, "")
	if err != nil {
		return func() {}, errors.Wrapf(err, "Failed mounting %s onto %s", source, dest)
	}
	return func() { syscall.Unmount(dest, syscall.MNT_DETACH) }, nil
}

type BySemver []string

func (a BySemver) Len() int      { return len(a) }
func (a BySemver) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a BySemver) Less(i, j int) bool {
	v1 := rpmversion.NewVersion(a[i])
	v2 := rpmversion.NewVersion(a[j])
	return v1.LessThan(v2)
}

func IsDir(d string) bool {
	f, err := os.Stat(d)
	return err == nil && f.IsDir()
}

func PathExists(d string) bool {
	_, err := os.Stat(d)
	if err != nil && os.IsNotExist(err) {
		return false
	}
	return true
}

func FileSize(f string) int64 {
	fi, err := os.Stat(f)
	if err != nil {
		return -1
	}
	return fi.Size()
}

func GetFileOwnership(filepath string) (uint32, uint32, error) {
	fileInfo, err := os.Stat(filepath)
	if err != nil {
		return 0, 0, errors.Wrapf(err, "Failed to stat file: %s", filepath)
	}

	fileSys := fileInfo.Sys()
	fileUID := fileSys.(*syscall.Stat_t).Uid
	fileGID := fileSys.(*syscall.Stat_t).Gid
	return fileUID, fileGID, nil
}

func WaitForPath(path string, retries, sleepSeconds int) bool {
	var numRetries int
	if retries == 0 {
		numRetries = 1
	} else {
		numRetries = retries
	}
	for i := 0; i < numRetries; i++ {
		if PathExists(path) {
			return true
		}
		time.Sleep(time.Duration(sleepSeconds) * time.Second)
	}
	return PathExists(path)
}

func ShaFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func ShaString(str string) (string, error) {
	h := sha256.New()
	n, err := h.Write([]byte(str))
	if err != nil {
		return "", errors.Wrapf(err, "couldn't write to hash object")
	}

	if n != len(str) {
		return "", errors.Errorf("didn't write all of %s", str)
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func ReadFileString(fileName string) (string, error) {
	if !PathExists(fileName) {
		return "", fmt.Errorf("No such file: %s", fileName)
	}

	content, err := ioutil.ReadFile(fileName)
	if err != nil {
		return "", errors.Wrapf(err, "Failed to read: %s", fileName)
	}

	return string(content), nil
}

// By default, atomix prints the stack trace for errors generated from
// pkg/errors via the %+v format string. We want to keep this behavior so that
// we can easily debug errors atomix encounters.
//
// However, some errors are really user errors, e.g. errors from hooks. For
// those, if we render a stack trace, people automatically e-mail us assuming
// it's our bug. So, we strip any stack trace to avoid it.
func StripStacktrace(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}

	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("%s: %v", msg, err)
}

const IgnoreCase = true
const UseCase = false

func InStringArray(needle string, haystack []string, ignoreCase bool) bool {
	if ignoreCase {
		needle = strings.ToLower(needle)
	}
	for _, cmp := range haystack {
		if ignoreCase {
			cmp = strings.ToLower(cmp)
		}
		if cmp == needle {
			return true
		}
	}
	return false
}

func MountSource(mountpoint string) (string, bool, error) {
	mounts, err := mount.ParseMounts("/proc/self/mountinfo")
	if err != nil {
		return "", false, err
	}

	for _, mount := range mounts {
		if mount.Target == mountpoint {
			return mount.Source, true, nil
		}
	}

	return "", false, nil
}

func GetPartitionFromDisk(diskDev string) string {
	partRe := regexp.MustCompile("[0-9]+$")
	return partRe.FindString(diskDev)
}

func GetDiskWithoutPartition(partDev string) (string, error) {
	// Figure it's gotta at least be "/dev/X1"
	if len(partDev) < 7 {
		return "", errors.New("partition device name too short")
	}

	var diskRe *regexp.Regexp
	nvmeRe := regexp.MustCompile("/nvme.*")
	if nvmeRe.MatchString(partDev) {
		diskRe = regexp.MustCompile("p[0-9]+$")
	} else {
		diskRe = regexp.MustCompile("[0-9]+$")
	}
	disk := diskRe.ReplaceAllString(partDev, "")
	return disk, nil
}

func GetDiskWithPartition(diskDev string, partNum int) (string, error) {
	// Figure it's gotta at least be "/dev/X"
	if len(diskDev) < 6 {
		return "", errors.New("disk device name too short")
	}

	partDev := ""
	nvmeRe := regexp.MustCompile("/nvme.*")
	if nvmeRe.MatchString(diskDev) {
		partDev = fmt.Sprintf("%sp%d", diskDev, partNum)
	} else {
		partDev = fmt.Sprintf("%s%d", diskDev, partNum)
	}
	return partDev, nil
}

func portAvail(p int) bool {

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func NextFreePort(first int) int {
	for p := first; ; p++ {
		if portAvail(p) {
			return p
		}
	}
}

func ReadLines(filename string) []string {
	lines := []string{}
	f, err := os.Open(filename)
	if err != nil {
		return lines
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

func VersionFromTag(tag string) string {

	// trs: each entry is a list of regexes that match a release
	// series and a function that translates between the tag and
	// the number within that series
	trs := []struct {
		res []*regexp.Regexp
		tr  func(tag string, submatches []string) string
	}{
		// apic ganga all = 1.2
		{[]*regexp.Regexp{
			regexp.MustCompile(`^rel/ganga/mr[\d].[\d]+.[\d]+-g[[:xdigit:]]{7,}-*[[:alpha:]]*$`)},
			func(tag string, matches []string) string { return "1.2" },
		},
		// apic hudson all = 2.1
		{[]*regexp.Regexp{
			regexp.MustCompile(`^2.2[\d].[\d]-h([\d]+)$`),
			regexp.MustCompile(`^rel/hudson/mr([\d]+.[\d]+)$`),
			regexp.MustCompile(`^2.2[\d].[\d]-hplus([\d]+)$`),
		},
			func(tag string, matches []string) string { return "2.1" },
		},
		// CASE hudson (CASE images 1.0.1x) = 3.x
		{[]*regexp.Regexp{
			regexp.MustCompile(`^[\d]{4}-[\d]{2}-[\d]{2}.[\d]+-se-h([\d]+)$`),
		},
			func(tag string, matches []string) string { return fmt.Sprintf("3.%s", matches[1]) },
		},
		// CASE test images from master after rel/se/hudson branch was cut
		// (CASE images 1.1.1x) = 3.99.$month.$day
		{[]*regexp.Regexp{
			regexp.MustCompile(`^[\d]{4}-([\d]{2})-([\d]{2}).[\d]+-[\d]+-g[[:xdigit:]]{7,}-*[[:alpha:]]*`),
		},
			func(tag string, matches []string) string { return fmt.Sprintf("3.99.%s.%s", matches[1], matches[2]) },
		},
		// apic indus = 4.x
		{[]*regexp.Regexp{
			regexp.MustCompile(`^2.2[\d].[\d]-i([\d]+)$`),
		},
			func(tag string, matches []string) string { return fmt.Sprintf("4.%s", matches[1]) },
		},
		// shorthand tags for dev releases e.g. 6-dev9
		{[]*regexp.Regexp{
			regexp.MustCompile(`^([\d]+)-dev([\d]+)$`),
		},
			func(tag string, matches []string) string {
				return fmt.Sprintf("%s-rc%s", matches[1], matches[2])
			},
		},
		// shorthand tags for dev releases with a branch postfix e.g. 6-dev9+centos8test1
		{[]*regexp.Regexp{
			regexp.MustCompile(`^([\d]+)-dev([\d]+)\+([[:alnum:]]*[[:alpha:]][[:digit:]]+)$`),
		},
			func(tag string, matches []string) string {
				return fmt.Sprintf("%s-rc%s+%s", matches[1], matches[2], matches[3])
			},
		},
		// dev tags from releases on master
		{[]*regexp.Regexp{
			regexp.MustCompile(`^([\d]+)-dev([\d]+)-([\d]+)-g[[:xdigit:]]{7,}$`),
		},
			func(tag string, matches []string) string {
				return fmt.Sprintf("%s-rc%s.%s", matches[1], matches[2], matches[3])
			},
		},
		// untagged dev versions from master with patches applied after a release
		// e.g. 5-dev3-16-g1b49e7c-dirty
		// we map this to 5-rc3.1 as a dev convenience, just so it sorts as newer than the 5-dev3 release.
		// because of rebasing, there's no good way to map the '16' into something useful.
		{[]*regexp.Regexp{
			regexp.MustCompile(`^([\d]+)-dev([\d]+)-([\d]+)-g[[:xdigit:]]{7,}-*[[:alpha:]]*$`),
		},
			func(tag string, matches []string) string {
				return fmt.Sprintf("%s-rc%s.%s.1", matches[1], matches[2], matches[3])
			},
		},
	}

	for _, ts := range trs {
		match := false
		submatches := []string{}

		for _, re := range ts.res {
			submatches = re.FindStringSubmatch(tag)
			if submatches != nil {
				match = true
				break
			}
		}
		if match {
			s := ts.tr(tag, submatches)
			return s
		}
	}
	return tag
}

// return true if a is deemed to be a newer version than b
// Complications: we've fussed around with atomix --version.
// Cases I know of:
//   * rel/ganga/mr1.2-1-ge871c53-dirty
//   * 2.28.0-hplus6
//   * 2019-05-22.0-9-g233976fb-dirty

func NewerVersion(a, b string) (bool, error) {
	av, err := version.NewVersion(VersionFromTag(a))
	if err != nil {
		return false, errors.WithStack(err)
	}
	bv, err := version.NewVersion(VersionFromTag(b))
	if err != nil {
		return false, errors.WithStack(err)
	}

	// work around go-version's handling of prerelease strings
	// (the stuff after -, e.g. rc9 in 8.2-rc9)
	// it has two components, prerelease and metadata. in 8.2-rc9+mike,
	// rc9 is "prerelease" and "mike" is metadata.
	//
	// go version ignores metadata for sorting, but we don't want to do that.

	// it splits prerelease stuff by '.' first, then sorts
	// based on each component, either numerically if the part is
	// just a number, or lexicographically otherwise.

	// so we re-create the version with the prerelease, but without 'rc':
	// 8.0.1-rc1 -> 8.0.1-1
	// and if there is metadata, we add it to the prerelease
	// section, but prepend a number that forces go-version to
	// sort something without metadata as "older" than something
	// with metadata:
	// 12-rc1+mike -> 12-1.1.mike (prerelease is "1.1.mike", sorts as (1, 1, mike) )
	// 12-rc1      -> 12-1.0      (prerelease is "1.0", sorts as (1,0) )
	// this tweak is required, because 12-1 will sort as newer
	// than 12-1.1.mike otherwise.

	avsegs := av.Segments()
	avpnum := strings.TrimPrefix(av.Prerelease(), "rc")
	avmeta := ".0"
	if len(av.Metadata()) > 0 {
		avmeta = fmt.Sprintf(".1.%s", av.Metadata())
	}
	avnps := fmt.Sprintf("%d.%d.%d-%s%s", avsegs[0], avsegs[1], avsegs[2], avpnum, avmeta)
	avnp, err := version.NewVersion(avnps)
	if err != nil {
		return false, errors.WithStack(err)
	}

	bvsegs := bv.Segments()
	bvpnum := strings.TrimPrefix(bv.Prerelease(), "rc")
	bvmeta := ".0"
	if len(bv.Metadata()) > 0 {
		bvmeta = fmt.Sprintf(".1.%s", bv.Metadata())
	}
	bvnps := fmt.Sprintf("%d.%d.%d-%s%s", bvsegs[0], bvsegs[1], bvsegs[2], bvpnum, bvmeta)
	bvnp, err := version.NewVersion(bvnps)
	if err != nil {
		return false, errors.WithStack(err)
	}

	return avnp.GreaterThan(bvnp), nil
}

// Return the version string for the atomix binary at 'path'.
// If chroot != "", then path should not include chroot.
func GetAtomixVersion(path string, chroot string) (string, error) {
	cmd := []string{path, "--version"}
	if chroot != "" {
		cmd = append([]string{"chroot", chroot}, cmd...)
	}
	stdout, stderr, rc := RunCommandWithOutputErrorRc(cmd...)
	if rc != 0 {
		return "0.0-ERROR", fmt.Errorf("Execution failed %d: %v, stdout: %v stderr: %v", rc, cmd, string(stdout), string(stderr))
	}
	stringOut := strings.TrimRight(string(stdout), "\n")
	return strings.ReplaceAll(stringOut, "atomix version ", ""), nil
}

func MountTmpfs(dest, size string) error {
	if err := EnsureDir(dest); err != nil {
		return errors.Wrapf(err, "Failed making mount point")
	}
	flags := uintptr(syscall.MS_NODEV | syscall.MS_NOSUID | syscall.MS_NOEXEC)
	return syscall.Mount("tmpfs", dest, "tmpfs", flags, "size="+size)
}

func RunWithStdin(stdinString string, args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return errors.Errorf("%s: %s", strings.Join(args, " "), err)
	}
	go func() {
		defer stdin.Close()
		io.WriteString(stdin, stdinString)
	}()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Errorf("%s: %s: %s", strings.Join(args, " "), err, string(output))
	}
	return nil
}

func RunWithStdall(stdinString string, args ...string) (string, string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", "", errors.Wrapf(err, "Failed getting stdin pipe %v", args)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	go func() {
		defer stdin.Close()
		io.WriteString(stdin, stdinString)
	}()
	err = cmd.Run()
	return stdout.String(), stderr.String(), err
}

// Create a tmpfile, write contents to it, close it, return
// the filename.
func WriteTempFile(dir, prefix, contents string) (string, error) {
	f, err := ioutil.TempFile(dir, prefix)
	if err != nil {
		return "", errors.Wrapf(err, "Failed opening a tempfile")
	}
	name := f.Name()
	_, err = f.Write([]byte(contents))
	defer f.Close()
	return name, errors.Wrapf(err, "Failed writing contents to tempfile")
}

const mke2fsConf string = `
[defaults]
base_features = sparse_super,filetype,resize_inode,dir_index,ext_attr
default_mntopts = acl,user_xattr
enable_periodic_fsck = 0
blocksize = 4096
inode_size = 256
inode_ratio = 16384

[fs_types]
ext4 = {
	features = has_journal,extent,huge_file,flex_bg,uninit_bg,dir_nlink,extra_isize,64bit
	inode_size = 256
}
small = {
	blocksize = 1024
	inode_size = 128
	inode_ratio = 4096
}
big = {
	inode_ratio = 32768
}
huge = {
	inode_ratio = 65536
}
`

type MkExt4Opts struct {
	Label    string
	Features []string
	ExtOpts  []string
}

func MkExt4FS(path string, opts MkExt4Opts) error {
	conf, err := WriteTempFile("/tmp", "mkfsconf-", mke2fsConf)
	if err != nil {
		return err
	}
	defer os.Remove(conf)

	cmd := []string{"mkfs.ext4", "-F"}

	if opts.ExtOpts != nil {
		cmd = append(cmd, "-E"+strings.Join(opts.ExtOpts, ","))
	}

	if opts.Features != nil {
		cmd = append(cmd, "-O"+strings.Join(opts.Features, ","))
	}

	if opts.Label != "" {
		cmd = append(cmd, "-L"+opts.Label)
	}

	cmd = append(cmd, path)
	return RunCommandEnv(append(os.Environ(), "MKE2FS_CONFIG="+conf), cmd...)
}

func UserDataDir() (string, error) {
	p, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(p, ".local", "share"), nil
}

func DataDir(cluster string) string {
	return filepath.Join(dataDir, "machine", cluster)
}

func RunDir(cluster, vm string) string {
	return filepath.Join(DataDir(cluster), fmt.Sprintf("%s.rundir", vm))
}

func ApiSockPath(cluster string) string {
	return DataDir(cluster) + "/api.sock"
}

func ConfPath(cluster string) string {
	return filepath.Join(configDir, "machine", cluster, "machine.yaml")
}

func SockPath(cluster, vm, sock string) string {
	return filepath.Join(RunDir(cluster, vm), "sockets", sock)
}
