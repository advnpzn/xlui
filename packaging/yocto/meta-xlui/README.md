# meta-xlui

This layer builds the xlui terminal client from a pinned Git revision for OpenEmbedded-Core wrynose (Yocto 6.0). The recipe installs the static executable at `/usr/bin/xlui`. Select the target architecture through your build's `MACHINE`.

From an initialized build environment, add the layer and build the package.

```sh
bitbake-layers add-layer /path/to/xlui/packaging/yocto/meta-xlui
bitbake xlui
```

To include it in an image, add the following to the image recipe or build configuration, then rebuild that image.

```bitbake
IMAGE_INSTALL:append = " xlui"
```

Choose the runtime tools for the image's role.

| Connection | Image contents and access |
| --- | --- |
| Local Dom0 | Supply the Xen toolstack's `xl` and an authorized account. Guest memory readings also use `xenstore-read` when available. Guest definition writes require `sh`, `flock` and GNU coreutils. |
| Remote Dom0 | Include the OpenSSH client and provision host keys in the operator's known-hosts file. The remote Dom0 supplies the Xen tools. |
| XCP-ng or XenServer | Include a trusted CA bundle for the pool coordinator, or provision a private CA file for the connection profile. |

## Fetching sources

The build host needs authorized Git access to the repository. BitBake fetches the pinned source and checksum-verified Go modules before compilation. For a local Git mirror containing the specified commit, set the following in `local.conf`.

```bitbake
XLUI_SRC_URI:pn-xlui = "git:///srv/git/xlui.git;protocol=file;nobranch=1"
```

For disconnected builds, populate `DL_DIR` with `bitbake xlui --runall=fetch` while the sources are reachable. Reuse that download directory and set `BB_NO_NETWORK = "1"` for the offline build. A full image also needs its other recipes' source downloads.

When upgrading, update `SRCREV`, review the pinned revision's Go toolchain requirements and regenerate the dependency and license includes with `bitbake xlui -c update_modules`. Review the generated checksums and licenses before rebuilding.
