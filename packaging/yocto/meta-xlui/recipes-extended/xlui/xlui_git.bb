SUMMARY = "Terminal client for Xen hosts and pools"
HOMEPAGE = "https://github.com/advnpzn/xlui"
LICENSE = "CLOSED"
PV = "0.1.0~alpha1+git"

inherit go-mod go-mod-update-modules

GO_IMPORT = "github.com/advnpzn/xlui"
GO_INSTALL = "${GO_IMPORT}/cmd/xlui"
XLUI_SRC_URI ?= "git://github.com/advnpzn/xlui.git;protocol=https;branch=master"
SRC_URI = "${XLUI_SRC_URI};destsuffix=${GO_SRCURI_DESTSUFFIX}"
SRCREV = "7f1181c031c7a38b3217b57472011343c7e0ad46"

require xlui-go-mods.inc
require xlui-licenses.inc

export CGO_ENABLED = "0"
export GOTOOLCHAIN = "local"
export GOPROXY:task-compile = "off"
GO_LINKSHARED = ""
GO_RPATH = ""
GO_LINUXLOADER = ""
GOBUILDFLAGS:remove = "-buildmode=pie"
GOBUILDFLAGS:append = " -mod=readonly -buildvcs=false"

do_install() {
    install -d ${D}${bindir}
    install -m 0755 ${B}/${GO_BUILD_BINDIR}/xlui ${D}${bindir}/xlui
}
