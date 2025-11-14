{
  "targets": [
    {
      "target_name": "privatemode_addon",
      "sources": [
        "src/addon/privatemode_addon.cc"
      ],
      "include_dirs": [
        "<!@(node -p \"require('node-addon-api').include\")",
        "<!@(node -p \"require('./package.json').config.libprivatemode + '/include'\")"
      ],
      "cflags_cc!": [ "-fno-exceptions" ],
      "msvs_settings": {
        "VCCLCompilerTool": {
          "ExceptionHandling": 1
        }
      },
      "xcode_settings": {
        "GCC_ENABLE_CPP_EXCEPTIONS": "YES"
      },
      "defines": [
        "NAPI_CPP_EXCEPTIONS"
      ]
    }
  ]
}
