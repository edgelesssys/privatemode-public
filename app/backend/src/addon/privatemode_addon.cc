#include <array>
#include <memory>
#include <napi.h>
#include <stdexcept>
#include <string>

#ifdef _WIN32
#include <windows.h>
#else
#include <dlfcn.h>
#endif

#include "libprivatemode.h"

using namespace std::string_literals;
using MsgBuffer = std::array<char, 512>;
using PrivatemodeStartProxyFunc = decltype(PrivatemodeStartProxy) *;

namespace {
class LibraryLoader {
  private:
    PrivatemodeStartProxyFunc startProxyFunc;

    static std::string getLibraryPath() {
        const char *const envPath = std::getenv("LIBPRIVATEMODE_PATH");
        if (!envPath) {
            throw std::runtime_error("LIBPRIVATEMODE_PATH environment variable not set");
        }
        return envPath;
    }

  public:
    LibraryLoader() : startProxyFunc{} {
        const std::string libPath = getLibraryPath();

#ifdef _WIN32
        const auto handle = LoadLibraryA(libPath.c_str());
        if (!handle) {
            MsgBuffer buffer{};
            snprintf(buffer.data(), buffer.size(), "Failed to load library: %s (error code: %lu)", libPath.c_str(), GetLastError());
            throw std::runtime_error(buffer.data());
        }
        startProxyFunc = reinterpret_cast<PrivatemodeStartProxyFunc>(GetProcAddress(handle, "PrivatemodeStartProxy"));
        if (!startProxyFunc) {
            MsgBuffer buffer{};
            snprintf(buffer.data(), buffer.size(), "Failed to find PrivatemodeStartProxy function (error code: %lu)", GetLastError());
            throw std::runtime_error(buffer.data());
        }
#else
        const auto handle = dlopen(libPath.c_str(), RTLD_LAZY);
        if (!handle) {
            MsgBuffer buffer{};
            snprintf(buffer.data(), buffer.size(), "Failed to load library: %s (%s)", libPath.c_str(), dlerror());
            throw std::runtime_error(buffer.data());
        }
        startProxyFunc = reinterpret_cast<PrivatemodeStartProxyFunc>(dlsym(handle, "PrivatemodeStartProxy"));
        if (!startProxyFunc) {
            MsgBuffer buffer{};
            snprintf(buffer.data(), buffer.size(), "Failed to find PrivatemodeStartProxy function (%s)", dlerror());
            throw std::runtime_error(buffer.data());
        }
#endif
    }

    PrivatemodeStartProxyFunc getStartProxyFunc() const noexcept {
        return startProxyFunc;
    }
};
} // namespace

static std::unique_ptr<LibraryLoader> libraryLoader;

static Napi::Object StartProxy(const Napi::CallbackInfo &info) {
    const Napi::Env env = info.Env();

    if (!libraryLoader) {
        Napi::Error::New(env, "Library not loaded").ThrowAsJavaScriptException();
        return Napi::Object::New(env);
    }

    const PrivatemodeStartProxyFunc func = libraryLoader->getStartProxyFunc();
    const PrivatemodeStartProxy_return result = func();

    Napi::Object returnObj = Napi::Object::New(env);

    if (result.r0 == -1) {
        returnObj.Set("success", Napi::Boolean::New(env, false));
        returnObj.Set("error", Napi::String::New(env, result.r1.p, result.r1.n));
        returnObj.Set("port", Napi::String::New(env, "-1"));
    } else {
        returnObj.Set("success", Napi::Boolean::New(env, true));
        returnObj.Set("port", Napi::String::New(env, std::to_string(result.r0)));
    }

    return returnObj;
}

static Napi::Object Init(Napi::Env env, Napi::Object exports) {
    try {
        libraryLoader = std::make_unique<LibraryLoader>();
    } catch (const std::exception &e) {
        Napi::Error::New(env, "Failed to initialize library: "s + e.what()).ThrowAsJavaScriptException();
        return exports;
    }

    exports.Set("startProxy", Napi::Function::New(env, StartProxy));
    return exports;
}

NODE_API_MODULE(privatemode_addon, Init)
