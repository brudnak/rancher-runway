#!/usr/bin/env swift

import AppKit
import Foundation

let scriptURL = URL(
    fileURLWithPath: CommandLine.arguments[0],
    relativeTo: URL(fileURLWithPath: FileManager.default.currentDirectoryPath)
).standardizedFileURL
let repoRoot = scriptURL.deletingLastPathComponent().deletingLastPathComponent()
let outputDir = repoRoot.appendingPathComponent("packaging/macos")
let sourcePNG = outputDir.appendingPathComponent("AppIcon-source.png")
let iconsetDir = outputDir.appendingPathComponent("AppIcon.iconset")
let basePNG = outputDir.appendingPathComponent("AppIcon-1024.png")
let icnsPath = outputDir.appendingPathComponent("AppIcon.icns")
let pngOutPath = (ProcessInfo.processInfo.environment["RANCHER_RUNWAY_ICON_PNG_OUT"] ?? ProcessInfo.processInfo.environment["HA_RANCHER_ICON_PNG_OUT"])
    .flatMap { $0.isEmpty ? nil : URL(fileURLWithPath: $0) }

try FileManager.default.createDirectory(at: outputDir, withIntermediateDirectories: true)
try? FileManager.default.removeItem(at: iconsetDir)
try FileManager.default.createDirectory(at: iconsetDir, withIntermediateDirectories: true)

guard let sourceImage = NSImage(contentsOf: sourcePNG) else {
    fatalError("failed to read source icon PNG at \(sourcePNG.path)")
}

let size = NSSize(width: 1024, height: 1024)
let image = NSImage(size: size)
image.lockFocus()

let bounds = NSRect(origin: .zero, size: size)
let iconMask = NSBezierPath(roundedRect: bounds.insetBy(dx: 12, dy: 12), xRadius: 210, yRadius: 210)

let w = sourceImage.size.width
let h = sourceImage.size.height
let cropSize = min(w, h) * 0.8788
let cropX = (w - cropSize) / 2
let cropY = (h - cropSize) / 2
let sourceCrop = NSRect(x: cropX, y: cropY, width: cropSize, height: cropSize)

NSGraphicsContext.current?.saveGraphicsState()
iconMask.addClip()
NSGraphicsContext.current?.imageInterpolation = .high
sourceImage.draw(in: bounds, from: sourceCrop, operation: .sourceOver, fraction: 1.0)
NSGraphicsContext.current?.restoreGraphicsState()

if let pngOutPath {
    let pngDir = pngOutPath.deletingLastPathComponent()
    try FileManager.default.createDirectory(at: pngDir, withIntermediateDirectories: true)
}

image.unlockFocus()

guard
    let tiff = image.tiffRepresentation,
    let bitmap = NSBitmapImageRep(data: tiff),
    let pngData = bitmap.representation(using: .png, properties: [:])
else {
    fatalError("failed to render icon PNG")
}
try pngData.write(to: basePNG)
if let pngOutPath {
    try pngData.write(to: pngOutPath)
}

let iconSizes: [(String, Int)] = [
    ("icon_16x16.png", 16),
    ("icon_16x16@2x.png", 32),
    ("icon_32x32.png", 32),
    ("icon_32x32@2x.png", 64),
    ("icon_128x128.png", 128),
    ("icon_128x128@2x.png", 256),
    ("icon_256x256.png", 256),
    ("icon_256x256@2x.png", 512),
    ("icon_512x512.png", 512),
    ("icon_512x512@2x.png", 1024)
]

for (name, pixels) in iconSizes {
    let destination = iconsetDir.appendingPathComponent(name)
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/sips")
    process.arguments = ["-z", "\(pixels)", "\(pixels)", basePNG.path, "--out", destination.path]
    process.standardOutput = FileHandle.nullDevice
    process.standardError = FileHandle.nullDevice
    try process.run()
    process.waitUntilExit()
    if process.terminationStatus != 0 {
        fatalError("sips failed for \(name)")
    }
}

let iconutil = Process()
iconutil.executableURL = URL(fileURLWithPath: "/usr/bin/iconutil")
iconutil.arguments = ["-c", "icns", iconsetDir.path, "-o", icnsPath.path]
try iconutil.run()
iconutil.waitUntilExit()
if iconutil.terminationStatus != 0 {
    fatalError("iconutil failed")
}

try? FileManager.default.removeItem(at: iconsetDir)
try? FileManager.default.removeItem(at: basePNG)
print("Rendered \(icnsPath.path)")
if let pngOutPath {
    print("Rendered \(pngOutPath.path)")
}
