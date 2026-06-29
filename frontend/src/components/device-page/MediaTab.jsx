import React, { useState } from "react";
import {
    Camera,
    Video,
    Square,
    Download,
    Trash2,
    Image as ImageIcon,
    Film,
    Grid,
    ListVideo
} from "lucide-react";

import "./MediaTab.css";

function MediaTab({
    recording,
    mediaFiles = [],
    takeScreenshot,
    startRecording,
    stopRecording,
    downloadMedia,
    deleteMedia
}) {
    const [subTab, setSubTab] = useState("screenshots");

    const screenshots = mediaFiles.filter(f => f.type === "image");
    const recordings = mediaFiles.filter(f => f.type === "video");

    const downloadAllScreenshots = () => {
        screenshots.forEach((s, index) => {
            setTimeout(() => downloadMedia(s), index * 300); // Stagger downloads slightly
        });
    };

    const downloadAllRecordings = () => {
        recordings.forEach((r, index) => {
            setTimeout(() => downloadMedia(r), index * 300);
        });
    };

    return (
        <div className="media-container">
            {/* Sub-navigation */}
            <div className="media-tabs">
                <button 
                    className={`media-tab ${subTab === "screenshots" ? "active" : ""}`}
                    onClick={() => setSubTab("screenshots")}
                >
                    <Camera size={16} />
                    Screenshots
                </button>
                <button 
                    className={`media-tab ${subTab === "recordings" ? "active" : ""}`}
                    onClick={() => setSubTab("recordings")}
                >
                    <Video size={16} />
                    Screen Records
                </button>
            </div>

            <div className="media-content">
                {subTab === "screenshots" && (
                    <div className="screenshots-view">
                        <div className="media-toolbar">
                            <button className="btn btn-primary" onClick={takeScreenshot}>
                                <Camera size={16} /> Capture Screenshot
                            </button>
                            {screenshots.length > 0 && (
                                <button className="btn btn-outline" onClick={downloadAllScreenshots}>
                                    <Download size={16} /> Download All ({screenshots.length})
                                </button>
                            )}
                        </div>

                        {screenshots.length === 0 ? (
                            <div className="empty-state">
                                <ImageIcon size={48} className="empty-icon" />
                                <h3>No screenshots yet</h3>
                                <p>Click the capture button to take a screenshot of the device screen.</p>
                            </div>
                        ) : (
                            <div className="screenshot-grid">
                                {screenshots.map((file) => (
                                    <div key={file.id} className="screenshot-card">
                                        <div className="screenshot-img-wrapper">
                                            <img src={file.url} alt={file.name} />
                                            <div className="screenshot-overlay">
                                                <button className="icon-btn-round" onClick={() => downloadMedia(file)} title="Download">
                                                    <Download size={18} />
                                                </button>
                                                <button className="icon-btn-round danger" onClick={() => deleteMedia(file)} title="Delete">
                                                    <Trash2 size={18} />
                                                </button>
                                            </div>
                                        </div>
                                        <div className="screenshot-info">
                                            <span className="screenshot-name" title={file.name}>{file.name}</span>
                                            <span className="screenshot-time">{file.time}</span>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>
                )}

                {subTab === "recordings" && (
                    <div className="recordings-view">
                        <div className="media-toolbar">
                            {!recording ? (
                                <button className="btn btn-danger" onClick={startRecording}>
                                    <Video size={16} /> Start Recording
                                </button>
                            ) : (
                                <button className="btn btn-primary pulse-btn" onClick={stopRecording}>
                                    <Square size={16} /> Stop Recording
                                </button>
                            )}

                            {recordings.length > 0 && (
                                <button className="btn btn-outline" onClick={downloadAllRecordings}>
                                    <Download size={16} /> Download All ({recordings.length})
                                </button>
                            )}
                        </div>

                        {recordings.length === 0 ? (
                            <div className="empty-state">
                                <Film size={48} className="empty-icon" />
                                <h3>No recordings yet</h3>
                                <p>Click start recording to capture a video of the device screen.</p>
                            </div>
                        ) : (
                            <div className="recording-list">
                                {recordings.map((file) => (
                                    <div key={file.id} className="recording-item">
                                        <div className="recording-icon">
                                            <Film size={24} />
                                        </div>
                                        <div className="recording-details">
                                            <span className="recording-name">{file.name}</span>
                                            <span className="recording-time">{file.time}</span>
                                        </div>
                                        <div className="recording-actions">
                                            <button className="icon-btn-flat" onClick={() => downloadMedia(file)}>
                                                <Download size={18} />
                                            </button>
                                            <button className="icon-btn-flat danger" onClick={() => deleteMedia(file)}>
                                                <Trash2 size={18} />
                                            </button>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>
                )}
            </div>
        </div>
    );
}

export default MediaTab;