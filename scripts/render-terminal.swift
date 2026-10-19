// Rasterize an ANSI frame from the real panel renderer with macOS system fonts.
import AppKit

guard CommandLine.arguments.count == 3 else {
    fatalError("Usage: swift scripts/render-terminal.swift frame.ansi frame.png")
}
let source = try String(contentsOfFile: CommandLine.arguments[1], encoding: .utf8)
let rows = source.components(separatedBy: "\n")
let font = NSFont(name: "Menlo-Regular", size: 18)!
let boldFont = NSFont(name: "Menlo-Bold", size: 18)!
let cell = ("M" as NSString).size(withAttributes: [.font: font]).width
let lineHeight: CGFloat = 22
let margin: CGFloat = 24
let escape = try NSRegularExpression(pattern: "\u{1b}\\[([0-9;]*)m")
let plain = escape.stringByReplacingMatches(in: source, range: NSRange(source.startIndex..., in: source), withTemplate: "")
let columns = plain.components(separatedBy: "\n").map { $0.count }.max()!
let width = Int(ceil(CGFloat(columns) * cell + margin * 2))
let height = Int(CGFloat(rows.count) * lineHeight + margin * 2)
let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: width * 2, pixelsHigh: height * 2, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
bitmap.size = NSSize(width: width, height: height)
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
let context = NSGraphicsContext.current!.cgContext
context.translateBy(x: 0, y: CGFloat(height))
context.scaleBy(x: 1, y: -1)
NSGraphicsContext.current = NSGraphicsContext(cgContext: context, flipped: true)
let base = NSColor(srgbRed: 28/255, green: 28/255, blue: 34/255, alpha: 1)
base.setFill()
context.fill(CGRect(x: 0, y: 0, width: width, height: height))
var foreground = NSColor(srgbRed: 192/255, green: 192/255, blue: 192/255, alpha: 1)
var background = base
var bold = false
for (row, text) in rows.enumerated() {
    let ns = text as NSString
    var cursor = 0
    var x = margin
    func draw(_ range: NSRange) {
        guard range.length > 0 else { return }
        let value = ns.substring(with: range)
        for character in value {
            let rect = CGRect(x: x, y: margin + CGFloat(row) * lineHeight, width: cell, height: lineHeight)
            background.setFill()
            context.fill(rect.integral)
            (String(character) as NSString).draw(at: NSPoint(x: x, y: rect.minY), withAttributes: [.font: bold ? boldFont : font, .foregroundColor: foreground])
            x += cell
        }
    }
    for match in escape.matches(in: text, range: NSRange(location: 0, length: ns.length)) {
        draw(NSRange(location: cursor, length: match.range.location - cursor))
        let codes = ns.substring(with: match.range(at: 1)).split(separator: ";").compactMap { Int($0) }
        var i = 0
        while i < codes.count {
            let code = codes[i]
            if (code == 38 || code == 48), i + 4 < codes.count, codes[i + 1] == 2 {
                let color = NSColor(srgbRed: CGFloat(codes[i + 2])/255, green: CGFloat(codes[i + 3])/255, blue: CGFloat(codes[i + 4])/255, alpha: 1)
                if code == 38 { foreground = color } else { background = color }
                i += 5
                continue
            }
            if code == 1 { bold = true }
            if code == 22 { bold = false }
            if code == 39 || code == 0 { foreground = NSColor(srgbRed: 192/255, green: 192/255, blue: 192/255, alpha: 1) }
            if code == 49 || code == 0 { background = base }
            i += 1
        }
        cursor = match.range.location + match.range.length
    }
    draw(NSRange(location: cursor, length: ns.length - cursor))
}
NSGraphicsContext.restoreGraphicsState()
try bitmap.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: CommandLine.arguments[2]))
