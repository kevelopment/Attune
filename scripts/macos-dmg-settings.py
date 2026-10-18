import os

files = [os.environ["ATTUNE_APP"]]
symlinks = {"Applications": "/Applications"}
background = os.environ["ATTUNE_DMG_BACKGROUND"]
format = "UDZO"
filesystem = "HFS+"
window_rect = ((100, 100), (760, 480))
icon_locations = {"Attune.app": (180, 250), "Applications": (580, 250)}
icon_size = 112
text_size = 14
show_status_bar = False
show_toolbar = False
show_sidebar = False
show_pathbar = False
include_icon_view_settings = "auto"
