import React from "react";
import {
    Camera,
    Video,
    Square,
    Download,
    Trash2,
    RefreshCw,
    Copy,
    Image,
    Film
} from "lucide-react";

import "./MediaTab.css";

function MediaTab({
    screenshot,
    recording,
    mediaFiles = [],
    takeScreenshot,
    startRecording,
    stopRecording,
    refreshMedia,
    downloadMedia,
    deleteMedia,
    copyPath,
}) {

    return (
        <div className="media-grid">

            {/* Screenshot */}

            <div className="media-card">

                <div className="media-header">
                    <span>
                        <Camera size={18} />
                        Screenshot
                    </span>

                    <button
                        className="icon-btn"
                        onClick={refreshMedia}
                    >
                        <RefreshCw size={16} />
                    </button>
                </div>

                <div className="preview-box">

                    {screenshot ? (
                        <img
                            src={screenshot.url}
                            alt=""
                        />
                    ) : (
                        <div className="placeholder">
                            <Image size={40} />
                            No Screenshot
                        </div>
                    )}

                </div>

                <div className="action-row">

                    <button
                        className="btn btn-primary"
                        onClick={takeScreenshot}
                    >
                        <Camera size={15} />
                        Capture
                    </button>

                    {screenshot && (
                        <>
                            <button
                                className="btn btn-ghost"
                                onClick={() => downloadMedia(screenshot)}
                            >
                                <Download size={15} />
                            </button>

                            <button
                                className="btn btn-ghost"
                                onClick={() => copyPath(screenshot.path)}
                            >
                                <Copy size={15} />
                            </button>
                        </>
                    )}

                </div>

            </div>

            {/* Recording */}

            <div className="media-card">

                <div className="media-header">
                    <span>
                        <Video size={18} />
                        Screen Recording
                    </span>

                    <span
                        className={
                            recording
                                ? "recording-status active"
                                : "recording-status"
                        }
                    >
                        {recording ? "Recording" : "Idle"}
                    </span>

                </div>

                <div className="record-preview">

                    <Film size={44} />

                    <h4>
                        {recording
                            ? "Recording..."
                            : "Ready"}
                    </h4>

                </div>

                <div className="action-row">

                    {!recording ? (
                        <button
                            className="btn btn-danger"
                            onClick={startRecording}
                        >
                            <Video size={15} />
                            Start
                        </button>
                    ) : (
                        <button
                            className="btn btn-primary"
                            onClick={stopRecording}
                        >
                            <Square size={15} />
                            Stop
                        </button>
                    )}

                </div>

            </div>

            {/* History */}

            <div className="media-history">

                <div className="media-header">
                    <span>Recent Media</span>
                </div>

                <div className="history-list">

                    {mediaFiles.length === 0 && (
                        <div className="empty-history">
                            No screenshots or recordings yet.
                        </div>
                    )}

                    {mediaFiles.map(file => (

                        <div
                            className="history-item"
                            key={file.id}
                        >

                            <div className="history-info">

                                {file.type === "image"
                                    ? <Image size={18}/>
                                    : <Film size={18}/>
                                }

                                <div>

                                    <div className="filename">
                                        {file.name}
                                    </div>

                                    <div className="date">
                                        {file.time}
                                    </div>

                                </div>

                            </div>

                            <div className="history-actions">

                                <button
                                    className="icon-btn"
                                    onClick={() => downloadMedia(file)}
                                >
                                    <Download size={16}/>
                                </button>

                                <button
                                    className="icon-btn"
                                    onClick={() => deleteMedia(file)}
                                >
                                    <Trash2 size={16}/>
                                </button>

                            </div>

                        </div>

                    ))}

                </div>

            </div>

        </div>
    );
}

export default MediaTab;