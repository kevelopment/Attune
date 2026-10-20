#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <portabledeviceapi.h>
#include <portabledevice.h>
#include <portabledevicetypes.h>
#include <propkey.h>
#include <propvarutil.h>
#include <wrl/client.h>

#include <algorithm>
#include <cstdint>
#include <cstdio>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <limits>
#include <string>
#include <vector>

#pragma comment(lib, "PortableDeviceGUIDs.lib")
#pragma comment(lib, "Ole32.lib")
#pragma comment(lib, "OleAut32.lib")
#pragma comment(lib, "Propsys.lib")

using Microsoft::WRL::ComPtr;

namespace {

std::wstring utf8_to_wide(const std::string& value) {
  if (value.empty()) return L"";
  int count = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, value.data(),
                                 static_cast<int>(value.size()), nullptr, 0);
  if (count <= 0) return L"";
  std::wstring out(static_cast<size_t>(count), L'\0');
  MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, value.data(),
                      static_cast<int>(value.size()), out.data(), count);
  return out;
}

std::string wide_to_utf8(const std::wstring& value) {
  if (value.empty()) return "";
  int count = WideCharToMultiByte(CP_UTF8, 0, value.data(),
                                  static_cast<int>(value.size()), nullptr, 0,
                                  nullptr, nullptr);
  if (count <= 0) return "";
  std::string out(static_cast<size_t>(count), '\0');
  WideCharToMultiByte(CP_UTF8, 0, value.data(),
                      static_cast<int>(value.size()), out.data(), count,
                      nullptr, nullptr);
  return out;
}

std::string json_string(const std::wstring& value) {
  const std::string utf8 = wide_to_utf8(value);
  std::string out = "\"";
  for (unsigned char c : utf8) {
    switch (c) {
      case '"': out += "\\\""; break;
      case '\\': out += "\\\\"; break;
      case '\b': out += "\\b"; break;
      case '\f': out += "\\f"; break;
      case '\n': out += "\\n"; break;
      case '\r': out += "\\r"; break;
      case '\t': out += "\\t"; break;
      default:
        if (c < 0x20) {
          char hex[7];
          std::snprintf(hex, sizeof(hex), "\\u%04x", c);
          out += hex;
        } else {
          out += static_cast<char>(c);
        }
    }
  }
  out += '"';
  return out;
}

std::string json_string_utf8(const std::string& value) {
  std::wstring wide = utf8_to_wide(value);
  return json_string(wide);
}

// The protocol request is a small JSON object. This parser handles JSON string
// escapes and integer fields without pulling in a runtime or third-party code.
class Request {
 public:
  explicit Request(const std::string& input) : input_(input) {}

  bool string(const char* key, std::string* value) const {
    size_t p = field(key);
    if (p == std::string::npos || p >= input_.size() || input_[p] != '"') return false;
    ++p;
    value->clear();
    while (p < input_.size()) {
      char c = input_[p++];
      if (c == '"') return true;
      if (c != '\\') { value->push_back(c); continue; }
      if (p >= input_.size()) return false;
      c = input_[p++];
      switch (c) {
        case '"': case '\\': case '/': value->push_back(c); break;
        case 'b': value->push_back('\b'); break;
        case 'f': value->push_back('\f'); break;
        case 'n': value->push_back('\n'); break;
        case 'r': value->push_back('\r'); break;
        case 't': value->push_back('\t'); break;
        case 'u': {
          if (p + 4 > input_.size()) return false;
          wchar_t wc = 0;
          for (int i = 0; i < 4; ++i) {
            const char h = input_[p++];
            wc = static_cast<wchar_t>(wc << 4);
            if (h >= '0' && h <= '9') wc += h - '0';
            else if (h >= 'a' && h <= 'f') wc += h - 'a' + 10;
            else if (h >= 'A' && h <= 'F') wc += h - 'A' + 10;
            else return false;
          }
          value->append(wide_to_utf8(std::wstring(1, wc)));
          break;
        }
        default: return false;
      }
    }
    return false;
  }

  bool integer(const char* key, uint64_t* value) const {
    size_t p = field(key);
    if (p == std::string::npos) return false;
    while (p < input_.size() && (input_[p] == ' ' || input_[p] == '\t')) ++p;
    if (p == input_.size() || input_[p] < '0' || input_[p] > '9') return false;
    uint64_t result = 0;
    while (p < input_.size() && input_[p] >= '0' && input_[p] <= '9') {
      unsigned digit = static_cast<unsigned>(input_[p++] - '0');
      if (result > (std::numeric_limits<uint64_t>::max() - digit) / 10) return false;
      result = result * 10 + digit;
    }
    *value = result;
    return true;
  }

 private:
  size_t field(const char* key) const {
    std::string token = std::string("\"") + key + "\"";
    size_t p = input_.find(token);
    if (p == std::string::npos) return p;
    p = input_.find(':', p + token.size());
    if (p == std::string::npos) return p;
    ++p;
    while (p < input_.size() && (input_[p] == ' ' || input_[p] == '\t')) ++p;
    return p;
  }

  const std::string& input_;
};

void reply_ok(const std::string& result = "null") {
  std::cout << "{\"ok\":true,\"result\":" << result << "}\n" << std::flush;
}

void reply_error(const std::string& message) {
  std::cout << "{\"ok\":false,\"error\":" << json_string_utf8(message)
            << "}\n" << std::flush;
}

std::string hresult_message(const char* operation, HRESULT hr) {
  wchar_t* text = nullptr;
  FormatMessageW(FORMAT_MESSAGE_ALLOCATE_BUFFER | FORMAT_MESSAGE_FROM_SYSTEM |
                     FORMAT_MESSAGE_IGNORE_INSERTS,
                 nullptr, static_cast<DWORD>(hr), 0,
                 reinterpret_cast<LPWSTR>(&text), 0, nullptr);
  std::wstring message = operation;
  if (text) { message += L": "; message += text; LocalFree(text); }
  else { wchar_t code[24]; swprintf_s(code, L" (0x%08X)", static_cast<unsigned>(hr)); message += code; }
  return wide_to_utf8(message);
}

bool get_string(IPortableDeviceValues* values, REFPROPERTYKEY key, std::wstring* out) {
  PWSTR raw = nullptr;
  if (FAILED(values->GetStringValue(key, &raw))) return false;
  *out = raw ? raw : L"";
  CoTaskMemFree(raw);
  return true;
}

uint64_t get_uint64(IPortableDeviceValues* values, REFPROPERTYKEY key) {
  ULONGLONG value = 0;
  return SUCCEEDED(values->GetUnsignedLargeIntegerValue(key, &value)) ? value : 0;
}

bool get_guid(IPortableDeviceValues* values, REFPROPERTYKEY key, GUID* out) {
  return SUCCEEDED(values->GetGuidValue(key, out));
}

class WPDHelper {
 public:
  HRESULT initialize() { return CoCreateInstance(CLSID_PortableDeviceManager, nullptr,
      CLSCTX_INPROC_SERVER, IID_PPV_ARGS(&manager_)); }

  bool dispatch(const std::string& line) {
    Request request(line);
    std::string op;
    if (!request.string("op", &op)) { reply_error("invalid WPD request"); return true; }
    if (op == "discover") { discover(); return true; }
    if (op == "open") { open(request); return true; }
    if (op == "storages") { storages(); return true; }
    if (op == "list") { list(request); return true; }
    if (op == "download") { download(request); return true; }
    if (op == "upload") { upload(request); return true; }
    if (op == "delete") { remove(request); return true; }
    if (op == "close") { close_device(); reply_ok(); return false; }
    if (op == "cancel") return true;
    reply_error("unsupported WPD operation");
    return true;
  }

  void shutdown() {
    close_device();
    properties_.Reset();
    manager_.Reset();
  }

 private:
  void discover() {
    DWORD count = 0;
    HRESULT hr = manager_->GetDevices(nullptr, &count);
    if (FAILED(hr)) { reply_error(hresult_message("count portable devices", hr)); return; }
    if (count == 0) { reply_ok("[]"); return; }
    std::vector<PWSTR> ids(count, nullptr);
    hr = manager_->GetDevices(ids.data(), &count);
    if (FAILED(hr)) { reply_error(hresult_message("enumerate portable devices", hr)); return; }
    std::string result = "[";
    bool first = true;
    for (DWORD i = 0; i < count; ++i) {
      if (!ids[i]) continue;
      DWORD name_chars = 0, vendor_chars = 0, product_chars = 0;
      manager_->GetDeviceFriendlyName(ids[i], nullptr, &name_chars);
      manager_->GetDeviceManufacturer(ids[i], nullptr, &vendor_chars);
      manager_->GetDeviceDescription(ids[i], nullptr, &product_chars);
      std::vector<wchar_t> name(name_chars ? name_chars : 1), vendor(vendor_chars ? vendor_chars : 1), product(product_chars ? product_chars : 1);
      if (name_chars) manager_->GetDeviceFriendlyName(ids[i], name.data(), &name_chars);
      if (vendor_chars) manager_->GetDeviceManufacturer(ids[i], vendor.data(), &vendor_chars);
      if (product_chars) manager_->GetDeviceDescription(ids[i], product.data(), &product_chars);
      if (!first) result += ',';
      first = false;
      result += "{\"id\":" + json_string(ids[i]) + ",\"name\":" + json_string(name.data()) +
          ",\"vendor\":" + json_string(vendor.data()) + ",\"product\":" + json_string(product.data()) + "}";
      CoTaskMemFree(ids[i]);
    }
    result += ']';
    reply_ok(result);
  }

  HRESULT get_content(ComPtr<IPortableDeviceContent>* content) {
    return device_ ? device_->Content(content->ReleaseAndGetAddressOf()) : E_UNEXPECTED;
  }

  bool open(const Request& request) {
    std::string id;
    if (!request.string("device", &id) || id.empty()) { reply_error("WPD device ID is required"); return true; }
    ComPtr<IPortableDevice> candidate;
    HRESULT hr = CoCreateInstance(CLSID_PortableDevice, nullptr, CLSCTX_INPROC_SERVER,
                                  IID_PPV_ARGS(&candidate));
    if (FAILED(hr)) { reply_error(hresult_message("create WPD device", hr)); return true; }
    ComPtr<IPortableDeviceValues> client;
    hr = CoCreateInstance(CLSID_PortableDeviceValues, nullptr, CLSCTX_INPROC_SERVER,
                          IID_PPV_ARGS(&client));
    if (SUCCEEDED(hr)) hr = client->SetUnsignedIntegerValue(CLIENT_DESIRED_ACCESS, GENERIC_READ | GENERIC_WRITE);
    std::wstring device_id = utf8_to_wide(id);
    if (SUCCEEDED(hr)) hr = candidate->Open(device_id.c_str(), client.Get());
    if (FAILED(hr)) { reply_error(hresult_message("open WPD device", hr)); return true; }
    device_ = candidate;
    reply_ok();
    return true;
  }

  bool storage_exists(IPortableDeviceContent* content, const std::wstring& storage) {
    ComPtr<IEnumPortableDeviceObjectIDs> objects;
    if (FAILED(content->EnumObjects(0, WPD_DEVICE_OBJECT_ID, nullptr, &objects))) return false;
    while (true) {
      DWORD fetched = 0;
      PWSTR id = nullptr;
      HRESULT hr = objects->Next(1, &id, &fetched);
      if (hr != S_OK || fetched == 0) { CoTaskMemFree(id); break; }
      bool match = storage == id;
      CoTaskMemFree(id);
      if (match) return true;
    }
    return false;
  }

  void storages() {
    ComPtr<IPortableDeviceContent> content;
    HRESULT hr = get_content(&content);
    if (FAILED(hr)) { reply_error(hresult_message("get WPD content", hr)); return; }
    ComPtr<IEnumPortableDeviceObjectIDs> objects;
    hr = content->EnumObjects(0, WPD_DEVICE_OBJECT_ID, nullptr, &objects);
    if (FAILED(hr)) { reply_error(hresult_message("enumerate WPD storage", hr)); return; }
    std::string result = "[";
    bool first = true;
    while (true) {
      DWORD fetched = 0;
      PWSTR id = nullptr;
      hr = objects->Next(1, &id, &fetched);
      if (hr != S_OK || fetched == 0) { CoTaskMemFree(id); break; }
      ComPtr<IPortableDeviceValues> values;
      hr = content->Properties(&properties_);
      if (SUCCEEDED(hr)) hr = properties_->GetValues(id, nullptr, &values);
      if (SUCCEEDED(hr)) {
        GUID category = GUID_NULL;
        if (get_guid(values.Get(), WPD_FUNCTIONAL_OBJECT_CATEGORY, &category) && IsEqualGUID(category, WPD_FUNCTIONAL_CATEGORY_STORAGE)) {
          std::wstring name;
          get_string(values.Get(), WPD_STORAGE_DESCRIPTION, &name);
          if (name.empty()) name = id;
          if (!first) result += ',';
          first = false;
          result += "{\"id\":" + json_string(id) + ",\"name\":" + json_string(name) +
              ",\"freeBytes\":" + std::to_string(get_uint64(values.Get(), WPD_STORAGE_FREE_SPACE_IN_BYTES)) +
              ",\"totalBytes\":" + std::to_string(get_uint64(values.Get(), WPD_STORAGE_CAPACITY)) + "}";
        }
      }
      CoTaskMemFree(id);
    }
    result += ']';
    reply_ok(result);
  }

  bool list_children(IPortableDeviceContent* content, const std::wstring& parent,
                     std::vector<std::string>* entries) {
    ComPtr<IEnumPortableDeviceObjectIDs> objects;
    HRESULT hr = content->EnumObjects(0, parent.c_str(), nullptr, &objects);
    if (FAILED(hr)) return false;
    while (true) {
      DWORD fetched = 0;
      PWSTR id = nullptr;
      hr = objects->Next(1, &id, &fetched);
      if (hr != S_OK || fetched == 0) { CoTaskMemFree(id); break; }
      ComPtr<IPortableDeviceValues> values;
      hr = content->Properties(&properties_);
      if (SUCCEEDED(hr)) hr = properties_->GetValues(id, nullptr, &values);
      if (SUCCEEDED(hr)) {
        std::wstring name;
        get_string(values.Get(), WPD_OBJECT_NAME, &name);
        if (entries) {
          GUID format = GUID_NULL;
          get_guid(values.Get(), WPD_OBJECT_CONTENT_TYPE, &format);
          const bool directory = IsEqualGUID(format, WPD_CONTENT_TYPE_FOLDER);
          const uint64_t size = get_uint64(values.Get(), WPD_OBJECT_SIZE);
          std::string entry = "{\"id\":" + json_string(id) + ",\"name\":" + json_string(name) +
              ",\"relativePath\":" + json_string(name) + ",\"size\":" + std::to_string(size) +
              ",\"directory\":" + (directory ? "true" : "false") + "}";
          entries->push_back(std::move(entry));
        }
      }
      CoTaskMemFree(id);
    }
    return true;
  }

  bool has_child_name(IPortableDeviceContent* content, const std::wstring& parent,
                      const std::wstring& target, HRESULT* error) {
    ComPtr<IEnumPortableDeviceObjectIDs> objects;
    *error = content->EnumObjects(0, parent.c_str(), nullptr, &objects);
    if (FAILED(*error)) return false;
    while (true) {
      DWORD fetched = 0;
      PWSTR id = nullptr;
      *error = objects->Next(1, &id, &fetched);
      if (*error != S_OK || fetched == 0) { CoTaskMemFree(id); *error = S_OK; return false; }
      ComPtr<IPortableDeviceProperties> properties;
      *error = content->Properties(&properties);
      ComPtr<IPortableDeviceValues> values;
      if (SUCCEEDED(*error)) *error = properties->GetValues(id, nullptr, &values);
      std::wstring name;
      if (SUCCEEDED(*error)) get_string(values.Get(), WPD_OBJECT_NAME, &name);
      CoTaskMemFree(id);
      if (FAILED(*error)) return false;
      if (!name.empty() && CompareStringOrdinal(name.c_str(), -1, target.c_str(), -1, TRUE) == CSTR_EQUAL) return true;
    }
  }

  void list(const Request& request) {
    std::string storage, parent;
    if (!request.string("storage", &storage) || storage.empty() || !request.string("parent", &parent)) { reply_error("WPD storage and parent IDs are required"); return; }
    ComPtr<IPortableDeviceContent> content;
    HRESULT hr = get_content(&content);
    if (FAILED(hr)) { reply_error(hresult_message("get WPD content", hr)); return; }
    std::wstring storage_wide = utf8_to_wide(storage);
    std::wstring parent_wide = parent.empty() ? storage_wide : utf8_to_wide(parent);
    if (!storage_exists(content.Get(), storage_wide)) { reply_error("WPD storage ID is unavailable"); return; }
    if (parent_wide != storage_wide && !valid_folder(content.Get(), storage_wide, parent_wide)) { reply_error("WPD parent folder is unavailable"); return; }
    std::vector<std::string> entries;
    if (!list_children(content.Get(), parent_wide, &entries)) { reply_error("enumerate WPD folder failed"); return; }
    std::string result = "[";
    for (size_t i = 0; i < entries.size(); ++i) { if (i) result += ','; result += entries[i]; }
    result += ']';
    reply_ok(result);
  }

  bool valid_object(IPortableDeviceContent* content, const std::wstring& storage, const std::wstring& object) {
    if (!storage_exists(content, storage)) return false;
    ComPtr<IPortableDeviceProperties> properties;
    if (FAILED(content->Properties(&properties))) return false;
    ComPtr<IPortableDeviceValues> values;
    if (FAILED(properties->GetValues(object.c_str(), nullptr, &values))) return false;
    std::wstring object_storage;
    return get_string(values.Get(), WPD_OBJECT_STORAGE_ID, &object_storage) && object_storage == storage;
  }

  bool valid_folder(IPortableDeviceContent* content, const std::wstring& storage, const std::wstring& object) {
    if (!valid_object(content, storage, object)) return false;
    ComPtr<IPortableDeviceProperties> properties;
    if (FAILED(content->Properties(&properties))) return false;
    ComPtr<IPortableDeviceValues> values;
    if (FAILED(properties->GetValues(object.c_str(), nullptr, &values))) return false;
    GUID type = GUID_NULL;
    return get_guid(values.Get(), WPD_OBJECT_CONTENT_TYPE, &type) && IsEqualGUID(type, WPD_CONTENT_TYPE_FOLDER);
  }

  void download(const Request& request) {
    std::string storage, object, path;
    if (!request.string("storage", &storage) || !request.string("object", &object) || !request.string("path", &path)) { reply_error("WPD download arguments are incomplete"); return; }
    ComPtr<IPortableDeviceContent> content;
    HRESULT hr = get_content(&content);
    if (SUCCEEDED(hr) && !valid_object(content.Get(), utf8_to_wide(storage), utf8_to_wide(object))) hr = E_INVALIDARG;
    ComPtr<IPortableDeviceProperties> object_properties;
    ComPtr<IPortableDeviceValues> object_values;
    if (SUCCEEDED(hr)) hr = content->Properties(&object_properties);
    if (SUCCEEDED(hr)) hr = object_properties->GetValues(utf8_to_wide(object).c_str(), nullptr, &object_values);
    ULONGLONG expected_size = 0;
    const bool expected_size_known = SUCCEEDED(hr) && SUCCEEDED(object_values->GetUnsignedLargeIntegerValue(WPD_OBJECT_SIZE, &expected_size));
    ComPtr<IPortableDeviceResources> resources;
    if (SUCCEEDED(hr)) hr = content->Transfer(&resources);
    DWORD optimal = 0;
    ComPtr<IStream> stream;
    if (SUCCEEDED(hr)) hr = resources->GetStream(utf8_to_wide(object).c_str(), WPD_RESOURCE_DEFAULT, STGM_READ, &optimal, &stream);
    if (FAILED(hr)) { reply_error(hresult_message("open WPD object stream", hr)); return; }
    std::ofstream file(std::filesystem::path(utf8_to_wide(path)), std::ios::binary | std::ios::trunc);
    if (!file) { reply_error("cannot create temporary download file"); return; }
    ULARGE_INTEGER total = {};
    STATSTG stat = {};
    if (SUCCEEDED(stream->Stat(&stat, STATFLAG_NONAME))) total = stat.cbSize;
    uint64_t done = 0;
    std::vector<char> buffer(std::clamp<DWORD>(optimal, 64 * 1024, 4 * 1024 * 1024));
    while (true) {
      if (take_cancel()) { file.close(); DeleteFileW(utf8_to_wide(path).c_str()); reply_error("operation cancelled"); return; }
      ULONG read = 0;
      hr = stream->Read(buffer.data(), static_cast<ULONG>(buffer.size()), &read);
      if (FAILED(hr)) { file.close(); DeleteFileW(utf8_to_wide(path).c_str()); reply_error(hresult_message("read WPD object", hr)); return; }
      if (!read) break;
      file.write(buffer.data(), read);
      if (!file) { file.close(); DeleteFileW(utf8_to_wide(path).c_str()); reply_error("write temporary download file failed"); return; }
      done += read;
      progress(done, total.QuadPart);
    }
    file.close();
    if (!file) { DeleteFileW(utf8_to_wide(path).c_str()); reply_error("finish temporary download file failed"); return; }
    if (expected_size_known && done != expected_size) {
      DeleteFileW(utf8_to_wide(path).c_str());
      reply_error("WPD download size mismatch"); return;
    }
    reply_ok();
  }

  void upload(const Request& request) {
    std::string storage, parent, name, path;
    uint64_t size = 0;
    if (!request.string("storage", &storage) || !request.string("parent", &parent) || !request.string("name", &name) ||
        !request.string("path", &path) || !request.integer("size", &size) || name.empty() || name.find_first_of("/\\") != std::string::npos) {
      reply_error("WPD upload arguments are invalid"); return;
    }
    std::ifstream file(std::filesystem::path(utf8_to_wide(path)), std::ios::binary);
    if (!file) { reply_error("cannot open temporary upload file"); return; }
    ComPtr<IPortableDeviceContent> content;
    HRESULT hr = get_content(&content);
    std::wstring storage_wide = utf8_to_wide(storage);
    std::wstring parent_wide = parent.empty() ? storage_wide : utf8_to_wide(parent);
    if (SUCCEEDED(hr) && !storage_exists(content.Get(), storage_wide)) hr = E_INVALIDARG;
    std::wstring name_wide = utf8_to_wide(name);
    if (SUCCEEDED(hr) && parent_wide != storage_wide && !valid_folder(content.Get(), storage_wide, parent_wide)) hr = E_INVALIDARG;
    HRESULT enum_error = S_OK;
    if (SUCCEEDED(hr) && has_child_name(content.Get(), parent_wide, name_wide, &enum_error)) {
      reply_error("an object with this name already exists"); return;
    }
    if (SUCCEEDED(hr) && FAILED(enum_error)) hr = enum_error;
    if (FAILED(hr)) { reply_error(hresult_message("validate WPD upload target", hr)); return; }
    ComPtr<IPortableDeviceValues> values;
    hr = CoCreateInstance(CLSID_PortableDeviceValues, nullptr, CLSCTX_INPROC_SERVER, IID_PPV_ARGS(&values));
    if (SUCCEEDED(hr)) hr = values->SetStringValue(WPD_OBJECT_PARENT_ID, parent_wide.c_str());
    if (SUCCEEDED(hr)) hr = values->SetStringValue(WPD_OBJECT_NAME, name_wide.c_str());
    if (SUCCEEDED(hr)) hr = values->SetStringValue(WPD_OBJECT_ORIGINAL_FILE_NAME, name_wide.c_str());
    if (SUCCEEDED(hr)) hr = values->SetGuidValue(WPD_OBJECT_CONTENT_TYPE, WPD_CONTENT_TYPE_DOCUMENT);
    if (SUCCEEDED(hr)) hr = values->SetGuidValue(WPD_OBJECT_FORMAT, WPD_OBJECT_FORMAT_UNSPECIFIED);
    if (SUCCEEDED(hr)) hr = values->SetUnsignedLargeIntegerValue(WPD_OBJECT_SIZE, size);
    ComPtr<IStream> stream;
    DWORD optimal_write_size = 0;
    if (SUCCEEDED(hr)) hr = content->CreateObjectWithPropertiesAndData(values.Get(), &stream, &optimal_write_size, nullptr);
    if (FAILED(hr)) { reply_error(hresult_message("create WPD object", hr)); return; }
    ComPtr<IPortableDeviceDataStream> data_stream;
    hr = stream.As(&data_stream);
    if (FAILED(hr)) { stream->Revert(); reply_error(hresult_message("open WPD object stream", hr)); return; }
    uint64_t done = 0;
    std::vector<char> buffer(std::clamp<DWORD>(optimal_write_size, 64 * 1024, 4 * 1024 * 1024));
    while (done < size) {
      if (take_cancel()) { stream->Revert(); reply_error("operation cancelled"); return; }
      size_t want = static_cast<size_t>(std::min<uint64_t>(buffer.size(), size - done));
      file.read(buffer.data(), static_cast<std::streamsize>(want));
      std::streamsize got = file.gcount();
      if (got <= 0) { stream->Revert(); reply_error("temporary upload data ended early"); return; }
      ULONG written = 0;
      hr = stream->Write(buffer.data(), static_cast<ULONG>(got), &written);
      if (FAILED(hr) || written != static_cast<ULONG>(got)) { stream->Revert(); reply_error(FAILED(hr) ? hresult_message("write WPD object", hr) : "short write to WPD object"); return; }
      done += static_cast<uint64_t>(written);
      progress(done, size);
    }
    if (take_cancel()) { stream->Revert(); reply_error("operation cancelled"); return; }
    hr = data_stream->Commit(STGC_DEFAULT);
    if (FAILED(hr)) { stream->Revert(); reply_error(hresult_message("commit WPD object", hr)); return; }
    PWSTR created_id_raw = nullptr;
    hr = data_stream->GetObjectID(&created_id_raw);
    std::wstring created_id = created_id_raw ? created_id_raw : L"";
    CoTaskMemFree(created_id_raw);
    if (FAILED(hr) || created_id.empty()) { reply_error(FAILED(hr) ? hresult_message("get created WPD object ID", hr) : "WPD returned an empty object ID after upload"); return; }
    std::string result = "{\"id\":" + json_string(created_id) + ",\"name\":" + json_string(name_wide) +
        ",\"relativePath\":" + json_string(name_wide) + ",\"size\":" + std::to_string(size) +
        ",\"directory\":false}";
    reply_ok(result);
  }

  void remove(const Request& request) {
    std::string storage, object;
    if (!request.string("storage", &storage) || !request.string("object", &object)) { reply_error("WPD delete arguments are incomplete"); return; }
    ComPtr<IPortableDeviceContent> content;
    HRESULT hr = get_content(&content);
    std::wstring storage_wide = utf8_to_wide(storage), object_wide = utf8_to_wide(object);
    if (SUCCEEDED(hr) && !valid_object(content.Get(), storage_wide, object_wide)) hr = E_INVALIDARG;
    ComPtr<IPortableDevicePropVariantCollection> ids;
    if (SUCCEEDED(hr)) hr = CoCreateInstance(CLSID_PortableDevicePropVariantCollection, nullptr, CLSCTX_INPROC_SERVER, IID_PPV_ARGS(&ids));
    PROPVARIANT value;
    PropVariantInit(&value);
    if (SUCCEEDED(hr)) {
      hr = InitPropVariantFromString(object_wide.c_str(), &value);
      if (SUCCEEDED(hr)) hr = ids->Add(&value);
      PropVariantClear(&value);
    }
    if (SUCCEEDED(hr)) hr = content->Delete(PORTABLE_DEVICE_DELETE_WITH_RECURSION, ids.Get(), nullptr);
    if (FAILED(hr)) { reply_error(hresult_message("delete WPD object", hr)); return; }
    reply_ok();
  }

  void progress(uint64_t done, uint64_t total) {
    std::cout << "{\"type\":\"progress\",\"done\":" << done << ",\"total\":" << total << "}\n" << std::flush;
  }

  bool take_cancel() {
    HANDLE input = GetStdHandle(STD_INPUT_HANDLE);
    DWORD available = 0;
    if (input == INVALID_HANDLE_VALUE || !PeekNamedPipe(input, nullptr, 0, nullptr, &available, nullptr) || available == 0) return false;
    std::string line;
    if (std::getline(std::cin, line)) {
      Request request(line);
      std::string operation;
      if (request.string("op", &operation) && operation == "cancel") return true;
    }
    return false;
  }

  void close_device() {
    if (device_) device_->Close();
    device_.Reset();
  }

  ComPtr<IPortableDeviceManager> manager_;
  ComPtr<IPortableDevice> device_;
  ComPtr<IPortableDeviceProperties> properties_;
};

}  // namespace

int wmain() {
  const HRESULT init = CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);
  if (FAILED(init)) { reply_error(hresult_message("initialize COM", init)); return 1; }
  WPDHelper helper;
  HRESULT hr = helper.initialize();
  if (FAILED(hr)) { reply_error(hresult_message("initialize WPD manager", hr)); CoUninitialize(); return 1; }
  std::string line;
  while (std::getline(std::cin, line)) {
    if (line.size() > 1024 * 1024) { reply_error("WPD request exceeds 1 MiB"); continue; }
    if (!helper.dispatch(line)) break;
  }
  helper.shutdown();
  CoUninitialize();
  return 0;
}
