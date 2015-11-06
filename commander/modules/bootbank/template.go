package bootbank

var (
	grubConfTemplateStr = `# AUTOGENERATED by Commander

set default="{{ .DefaultBootEntry }}"
set timeout=3

set menu_color_normal=white/black
set menu_color_highlight=black/light-gray

# Try the timeout_style feature, fallback to simulated countdown
if [ x$feature_timeout_style = xy ] ; then
  set timeout_style=hidden
  set timeout=5
elif sleep --verbose --interruptible {{ .HiddenTimeout }} ; then
  set timeout=0
fi

{{ range .BootbankEntries }}
menuentry "{{ .MenuEntry }}" {
	insmod  ext2
	search  --label --set=root --no-floppy {{ .PartitionLabel }}
	linux   /vmlinuz root=LABEL={{ .PartitionLabel }} {{ .KernelCmdlineOpts }}"
	initrd  /initrd.img
}
{{ end }}
`
)
