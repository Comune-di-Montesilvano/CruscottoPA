package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type categoryForm struct {
	ID   int64
	Name string
}

type categoriesSection struct {
	Categories []database.Category
	Form       categoryForm
	Errors     formErrors
}

func (s *Server) categoriesData(form categoryForm, errs formErrors) (categoriesSection, error) {
	cats, err := s.db.ListCategories()
	return categoriesSection{Categories: cats, Form: form, Errors: errs}, err
}

func (s *Server) renderCategories(w http.ResponseWriter, status int, form categoryForm, errs formErrors) {
	sec, err := s.categoriesData(form, errs)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "categories_section", sec)
}

func (s *Server) handleCategoriesPage(w http.ResponseWriter, r *http.Request) {
	sec, err := s.categoriesData(categoryForm{}, nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_categorie.html", "categorie", sec)
}

func (s *Server) handleCategoryEdit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := s.db.GetCategory(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderCategories(w, http.StatusOK, categoryForm{ID: c.ID, Name: c.Name}, nil)
}

func (s *Server) handleCategorySave(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	form := categoryForm{ID: id, Name: strings.TrimSpace(r.FormValue("name"))}
	errs := formErrors{}
	checkText(errs, "name", form.Name, 60, true)
	if len(errs) > 0 {
		s.renderCategories(w, http.StatusUnprocessableEntity, form, errs)
		return
	}
	if id == 0 {
		_, err = s.db.CreateCategory(form.Name)
	} else {
		err = s.db.UpdateCategory(id, form.Name)
	}
	switch {
	case errors.Is(err, database.ErrDuplicate):
		errs.add("name", "Esiste già una categoria con questo nome.")
		s.renderCategories(w, http.StatusUnprocessableEntity, form, errs)
	case errors.Is(err, database.ErrNotFound):
		http.NotFound(w, r)
	case err != nil:
		s.serverError(w, err)
	default:
		s.renderCategories(w, http.StatusOK, categoryForm{}, nil)
	}
}

func (s *Server) handleCategoryDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = s.db.DeleteCategory(id)
	switch {
	case errors.Is(err, database.ErrCategoryNotEmpty):
		s.renderCategories(w, http.StatusUnprocessableEntity, categoryForm{},
			formErrors{"general": "Sposta o elimina prima le app di questa categoria."})
	case errors.Is(err, database.ErrNotFound):
		http.NotFound(w, r)
	case err != nil:
		s.serverError(w, err)
	default:
		s.renderCategories(w, http.StatusOK, categoryForm{}, nil)
	}
}

func (s *Server) handleCategoryMove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.MoveCategory(id, moveDir(r)); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderCategories(w, http.StatusOK, categoryForm{}, nil)
}
