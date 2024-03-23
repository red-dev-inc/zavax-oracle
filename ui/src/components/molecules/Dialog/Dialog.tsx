import React from "react";
import TextLoadingIndicator from "../../atoms/TextLoadingIndicator";

interface DialogProps {
  loading: boolean;
  show: boolean;
  content: string;
  handleClose: () => void;
}
const Dialog: React.FC<DialogProps> = ({
  show,
  content,
  handleClose,
  loading,
}) => {
  return (
    <>
      {show && (
        <>
          <div
            className={`modal fade ${show ? "show" : ""}`}
            style={{ display: show ? "block" : "none" }}
            tabIndex={-1}
            role="dialog"
            aria-labelledby="exampleModalCenterTitle"
            aria-hidden={!show}
          >
            <div className="modal-dialog modal-dialog-centered" role="document">
              <div className="modal-content">
                <div className="modal-header">
                  <h5 className="modal-title">{""}</h5>
                    <span aria-hidden="true" className="close-icon" onClick={handleClose}>
                      &times;
                    </span>
                </div>
                <div className="modal-body">
                  {content}
                  <TextLoadingIndicator loading={loading} />
                </div>
                <div className="modal-footer">
                  {!loading && (
                    <button
                      type="button"
                      className="btn btn-danger"
                      onClick={handleClose}
                    >
                      OK
                    </button>
                  )}
                </div>
              </div>
            </div>
          </div>
          <div
            className="modal-backdrop fade show"
            style={{ display: "block" }}
          ></div>
        </>
      )}
    </>
  );
};

export default Dialog;
