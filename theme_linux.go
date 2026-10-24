//go:build linux && cgo

package main

/*
#cgo pkg-config: gtk+-3.0
#include <gio/gio.h>
#include <gtk/gtk.h>

static GSettings *attuneDesktopSettings;

static gboolean attuneSetGtkDarkPreference(gpointer data) {
	GtkSettings *settings = gtk_settings_get_default();
	if (settings != NULL) {
		g_object_set(settings, "gtk-application-prefer-dark-theme", GPOINTER_TO_INT(data), NULL);
	}
	return G_SOURCE_REMOVE;
}

static void attuneColorSchemeChanged(GSettings *settings, gchar *key, gpointer data) {
	gchar *scheme = g_settings_get_string(settings, key);
	if (g_strcmp0(scheme, "prefer-dark") == 0) {
		g_idle_add(attuneSetGtkDarkPreference, GINT_TO_POINTER(TRUE));
	} else if (g_strcmp0(scheme, "prefer-light") == 0) {
		g_idle_add(attuneSetGtkDarkPreference, GINT_TO_POINTER(FALSE));
	}
	g_free(scheme);
}

static gboolean attuneWatchSystemColorScheme(gpointer data) {
	GSettingsSchemaSource *source = g_settings_schema_source_get_default();
	GSettingsSchema *schema = source ? g_settings_schema_source_lookup(source, "org.gnome.desktop.interface", TRUE) : NULL;
	if (schema == NULL || !g_settings_schema_has_key(schema, "color-scheme")) {
		if (schema != NULL) g_settings_schema_unref(schema);
		return G_SOURCE_REMOVE;
	}

	attuneDesktopSettings = g_settings_new_full(schema, NULL, NULL);
	g_settings_schema_unref(schema);
	attuneColorSchemeChanged(attuneDesktopSettings, "color-scheme", NULL);
	g_signal_connect(attuneDesktopSettings, "changed::color-scheme", G_CALLBACK(attuneColorSchemeChanged), NULL);
	return G_SOURCE_REMOVE;
}

static void attuneConfigureSystemAppearance(void) {
	g_idle_add(attuneWatchSystemColorScheme, NULL);
}
*/
import "C"

func configureLinuxAppearance() {
	C.attuneConfigureSystemAppearance()
}
